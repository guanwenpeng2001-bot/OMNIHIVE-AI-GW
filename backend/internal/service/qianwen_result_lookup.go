package service

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

var (
	qianwenResultSystemLookup = net.DefaultResolver.LookupIP
	qianwenResultDNSClient    = newQianwenResultDNSClient((&net.Dialer{Timeout: 5 * time.Second}).DialContext)
	qianwenResultFakeIPv4     = netip.MustParsePrefix("198.18.0.0/16")
	qianwenResultFakeIPv6     = netip.MustParsePrefix("fdfe:dcba:9876::/64")
)

// Cloudflare's JSON DoH endpoint is documented at:
// https://developers.cloudflare.com/1.1.1.1/encryption/dns-over-https/make-api-requests/dns-json/
// Bootstrap without system DNS or environment/account proxies; authenticate the
// original resolver hostname over TLS. The dial argument permits offline tests.
func newQianwenResultDNSClient(dial func(context.Context, string, string) (net.Conn, error)) *http.Client {
	return &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return dial(ctx, network, "1.1.1.1:443")
			},
			TLSClientConfig:       &tls.Config{ServerName: "cloudflare-dns.com", MinVersion: tls.VersionTLS12},
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: 5 * time.Second,
			IdleConnTimeout:       30 * time.Second,
			DisableCompression:    true,
		},
	}
}

func withQianwenResultLookup(ctx context.Context, rawURL string) context.Context {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" || parsed.Host != parsed.Hostname() || net.ParseIP(parsed.Hostname()) != nil || !isQianwenVideoResultHost(parsed.Hostname()) {
		return ctx
	}
	return httpclient.WithPublicIPLookup(ctx, lookupQianwenResultIP)
}

func isQianwenResultFakeIP(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	return ok && (qianwenResultFakeIPv4.Contains(address.Unmap()) || qianwenResultFakeIPv6.Contains(address.Unmap()))
}

func lookupQianwenResultIP(ctx context.Context, network, host string) ([]net.IP, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ips, err := qianwenResultSystemLookup(ctx, network, host)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("Qianwen result DNS returned no addresses")
	}
	fake := false
	for _, ip := range ips {
		if isQianwenResultFakeIP(ip) {
			fake = true
			continue
		}
		// A FakeIP must never excuse a separate private/invalid DNS answer.
		if ip == nil || urlvalidator.IsBlockedHost(ip.String()) {
			return nil, fmt.Errorf("Qianwen result DNS contains a blocked address")
		}
	}
	if !fake {
		return ips, nil
	}
	if net.ParseIP(host) != nil || !isQianwenVideoResultHost(host) {
		return nil, fmt.Errorf("Qianwen result DNS fallback is not allowed for this host")
	}
	var resolved []net.IP
	remainingBytes := 16 << 10
	for _, queryType := range []string{"A", "AAAA"} {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		query := url.Values{"name": {host}, "type": {queryType}}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://cloudflare-dns.com/dns-query?"+query.Encode(), nil)
		if err != nil {
			return nil, err
		}
		request.Header.Set("Accept", "application/dns-json")
		response, err := qianwenResultDNSClient.Do(request)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, int64(remainingBytes)+1))
		_ = response.Body.Close()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if readErr != nil {
			return nil, readErr
		}
		if response.StatusCode != http.StatusOK || len(body) > remainingBytes {
			return nil, fmt.Errorf("Qianwen result DNS response unavailable or oversized")
		}
		remainingBytes -= len(body)
		var answer struct {
			Status *int
			TC     bool
			Answer []struct {
				Type int
				Data string
			}
		}
		if json.Unmarshal(body, &answer) != nil || answer.Status == nil || *answer.Status != 0 || answer.TC {
			return nil, fmt.Errorf("Qianwen result DNS response invalid")
		}
		for _, record := range answer.Answer {
			if record.Type != 1 && record.Type != 28 {
				continue
			}
			ip := net.ParseIP(record.Data)
			if ip == nil || !ip.IsGlobalUnicast() || (record.Type == 1) != (ip.To4() != nil) || isQianwenResultFakeIP(ip) || urlvalidator.IsBlockedHost(ip.String()) {
				return nil, fmt.Errorf("Qianwen result DNS returned a blocked address")
			}
			resolved = append(resolved, ip)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(resolved) == 0 {
		return nil, fmt.Errorf("Qianwen result DNS returned no addresses")
	}
	return resolved, nil
}
