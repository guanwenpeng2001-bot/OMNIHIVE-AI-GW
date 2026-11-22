package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/httpclient"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/util/urlvalidator"
)

const maxOIDCResponseBytes = 1 << 20

var oidcTransport = httpclient.NewPublicTransport(&http.Transport{ForceAttemptHTTP2: true})
var oidcDiscoveryClient = newOIDCClient(15*time.Second, true)
var oidcEndpointClient = newOIDCClient(30*time.Second, false)

func validateOIDCEndpoint(target *url.URL) error {
	if target == nil || target.User != nil || target.Fragment != "" {
		return errors.New("invalid OIDC endpoint")
	}
	if _, err := urlvalidator.ValidateHTTPSURL(target.String(), urlvalidator.ValidationOptions{}); err != nil {
		return errors.New("OIDC endpoint must use HTTPS and a public host")
	}
	return nil
}

func newOIDCClient(timeout time.Duration, allowDiscoveryRedirect bool) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: oidcTransport,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if err := validateOIDCEndpoint(r.URL); err != nil {
				return err
			}
			if !allowDiscoveryRedirect || len(via) >= 5 || !strings.EqualFold(r.URL.Host, via[0].URL.Host) {
				return errors.New("OIDC redirect is not allowed")
			}
			return nil
		},
	}
}

// ReadOIDCEndpoint sends token, UserInfo and JWKS requests without redirects.
func ReadOIDCEndpoint(ctx context.Context, method, target string, body io.Reader, headers http.Header) (int, []byte, error) {
	return readOIDCEndpoint(ctx, oidcEndpointClient, method, target, body, headers)
}

func readOIDCEndpoint(ctx context.Context, client *http.Client, method, target string, body io.Reader, headers http.Header) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimSpace(target), body)
	if err != nil {
		return 0, nil, errors.New("invalid OIDC request")
	}
	if err := validateOIDCEndpoint(req.URL); err != nil {
		return 0, nil, err
	}
	if headers != nil {
		req.Header = headers.Clone()
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		// net/http's error can include the complete request or redirect URL.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxOIDCResponseBytes+1))
	if len(data) > maxOIDCResponseBytes {
		return resp.StatusCode, nil, errors.New("OIDC response too large")
	}
	return resp.StatusCode, data, err
}
