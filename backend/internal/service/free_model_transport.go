package service

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
)

const freeModelBodyLimit = 32 << 20

type freeModelUpstream struct {
	base    HTTPUpstream
	account *Account
}

func (u *freeModelUpstream) Do(r *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	return u.do(r, func(req *http.Request) (*http.Response, error) { return u.base.Do(req, proxy, id, concurrency) })
}
func (u *freeModelUpstream) DoWithTLS(r *http.Request, proxy string, id int64, concurrency int, p *tlsfingerprint.Profile) (*http.Response, error) {
	return u.do(r, func(req *http.Request) (*http.Response, error) {
		return u.base.DoWithTLS(req, proxy, id, concurrency, p)
	})
}
func (u *freeModelUpstream) do(r *http.Request, send func(*http.Request) (*http.Response, error)) (*http.Response, error) {
	// Validate a local copy; cached account maps may be shared across requests.
	account := *u.account
	if err := NormalizeFreeModelCredentials(&account); err != nil {
		return nil, err
	}
	req := r.Clone(WithHTTPUpstreamRedirectsDisabled(r.Context()))
	var policy *zenFreeToolPolicy
	model := ""
	if req.Method == http.MethodPost && req.Body != nil {
		body, err := io.ReadAll(io.LimitReader(req.Body, freeModelBodyLimit+1))
		_ = req.Body.Close()
		if err != nil {
			return nil, err
		}
		if len(body) > freeModelBodyLimit {
			return nil, errors.New("free model request exceeds size limit")
		}
		if account.IsOpenCodeZenFree() {
			protocol := APIProtocolChatCompletions
			if strings.HasSuffix(req.URL.Path, "/responses") {
				protocol = APIProtocolResponses
			}
			if strings.HasSuffix(req.URL.Path, "/messages") {
				protocol = APIProtocolAnthropic
			}
			applyZenFreeHeaders(nil, req.Header, protocol == APIProtocolAnthropic, body)
			body, policy, err = prepareZenFreeRequest(&account, body, protocol)
		} else {
			body, model, err = prepareKiloChat(&account, body)
		}
		if err != nil {
			return nil, err
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
		req.ContentLength = int64(len(body))
		req.Header.Del("Content-Length")
	}
	if account.Platform == PlatformKilo {
		applyKiloHeaders(req.Header, &account)
	}
	if account.IsOpenCodeZenFree() && req.Method == http.MethodGet {
		applyZenFreeDiscoveryHeaders(req.Header)
	}
	resp, err := send(req)
	if err != nil || resp == nil {
		return resp, err
	}
	if account.Platform == PlatformKilo {
		if resp.StatusCode >= 200 && resp.StatusCode < 300 && !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
			rewritten, err := rewriteFreeModelResponse(resp, func(data []byte) ([]byte, error) {
				if gjson.GetBytes(data, "error").Exists() || openRouterStreamError(string(data)) {
					resp.StatusCode = http.StatusBadGateway
				}
				return normalizeKiloPayload(data, model, &account), nil
			})
			return rewritten, err
		}
		return rewriteFreeModelResponse(resp, func(data []byte) ([]byte, error) { return normalizeKiloPayload(data, model, &account), nil })
	}
	if policy != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return rewriteFreeModelResponse(resp, policy.guard)
	}
	return resp, nil
}

// Pull-based rewriting preserves backpressure and cancellation. Only one SSE
// event is read at a time; normal text and keepalive comments are never held
// behind buffered tool calls. JSON uses UseNumber so usage/cost decimals do not
// pass through floating point on either the wire or the billing path.
func decodeFreeModelObject(data []byte) (map[string]any, error) {
	var object map[string]any
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := d.Decode(&object); err != nil {
		return nil, err
	}
	if object == nil {
		return nil, errors.New("expected JSON object")
	}
	return object, nil
}
func rewriteFreeModelResponse(resp *http.Response, transform func([]byte) ([]byte, error)) (*http.Response, error) {
	if resp.Body == nil {
		return resp, nil
	}
	if resp.Header == nil {
		resp.Header = make(http.Header)
	}
	resp.Header.Del("Content-Length")
	resp.ContentLength = -1
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 64*1024), 4<<20)
		resp.Body = &freeModelSSEReader{source: resp.Body, scanner: scanner, transform: transform}
		return resp, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, freeModelBodyLimit+1))
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	if len(body) > freeModelBodyLimit {
		return nil, errors.New("free model response exceeds size limit")
	}
	body, err = transform(body)
	if err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	return resp, nil
}

type freeModelSSEReader struct {
	source    io.ReadCloser
	scanner   *bufio.Scanner
	transform func([]byte) ([]byte, error)
	pending   []byte
}

func (r *freeModelSSEReader) Close() error { return r.source.Close() }
func (r *freeModelSSEReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(r.pending) == 0 {
		var lines, data []string
		size := 0
		scanned := false
		for r.scanner.Scan() {
			scanned = true
			line := r.scanner.Text()
			size += len(line)
			if size > 4<<20 {
				return 0, errors.New("free model SSE event exceeds size limit")
			}
			if line == "" {
				break
			}
			if strings.HasPrefix(line, "data:") {
				data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			} else {
				lines = append(lines, line)
			}
			if len(lines) == 1 && len(data) == 0 && strings.HasPrefix(line, ":") {
				break
			}
		}
		if len(lines) == 0 && len(data) == 0 {
			if err := r.scanner.Err(); err != nil {
				return 0, err
			}
			if scanned {
				continue
			}
			return 0, io.EOF
		}
		if len(data) > 0 {
			payload := []byte(strings.Join(data, "\n"))
			{
				var err error
				payload, err = r.transform(payload)
				if err != nil {
					return 0, err
				}
				if payload == nil {
					continue
				}
			}
			lines = append(lines, "data: "+string(payload))
		}
		r.pending = []byte(strings.Join(lines, "\n") + "\n\n")
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}
