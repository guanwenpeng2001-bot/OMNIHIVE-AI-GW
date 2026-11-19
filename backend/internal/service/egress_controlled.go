package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/httpclient"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/trace"
)

var errEgress = errors.New("controlled egress unavailable")
var errEgressSize = errors.New("controlled response exceeds size limit")

// ControlledEgress is host-owned. Only a proxy chosen by the account routing
// policy reaches the constructor. Request headers and URLs cannot select it.
type ControlledEgress struct {
	client   *http.Client
	timeout  time.Duration
	maxBytes int64
}

func NewArtifactEgress() *ControlledEgress {
	return &ControlledEgress{client: &http.Client{Transport: httpclient.NewPublicTransport(&http.Transport{
		IdleConnTimeout: 30 * time.Second, MaxResponseHeaderBytes: 64 << 10,
	})}, timeout: 30 * time.Second, maxBytes: 32 << 20}
}
func providerEgress(proxy string) (*ControlledEgress, error) {
	c, err := httpclient.GetClient(httpclient.Options{ProxyURL: proxy, ValidateResolvedIP: true, ResponseHeaderTimeout: 30 * time.Second})
	if err != nil {
		return nil, errEgress
	}
	return &ControlledEgress{client: c, timeout: DefaultGenerationDeadline, maxBytes: 64 << 20}, nil
}
func validDestination(r *http.Request) bool {
	return r != nil && r.URL != nil && r.URL.Scheme == "https" && r.URL.Hostname() != "" && r.URL.User == nil && r.URL.Opaque == "" && (r.Host == "" || strings.EqualFold(r.Host, r.URL.Host))
}
func stripTrace(r *http.Request) {
	for _, key := range []string{"Baggage", "Tracestate", "Traceparent", "Referer", "Proxy-Authorization"} {
		r.Header.Del(key)
	}
}
func (e *ControlledEgress) Do(req *http.Request) (*http.Response, error) {
	client := *e.client
	client.Jar = nil
	client.CheckRedirect = func(r *http.Request, prior []*http.Request) error {
		if len(prior) > 3 || !validDestination(r) || prior[0].Method != "GET" || HTTPUpstreamRedirectsDisabled(r.Context()) {
			return errEgress
		}
		// Signed delivery URLs may redirect to another CDN. Forward no original headers.
		r.Header = make(http.Header)
		return nil
	}
	return e.send(req, client.Do)
}
func (e *ControlledEgress) send(req *http.Request, send func(*http.Request) (*http.Response, error)) (*http.Response, error) {
	if !validDestination(req) {
		return nil, errEgress
	}
	ctx, cancel := context.WithTimeout(trace.ContextWithSpanContext(baggage.ContextWithoutBaggage(req.Context()), trace.SpanContext{}), e.timeout)
	r := req.Clone(WithHTTPUpstreamPublicHostsOnly(ctx))
	stripTrace(r)
	resp, err := send(r)
	if err != nil {
		cancel()
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}
		if IsAttemptAdmissionError(err) {
			return nil, err
		}
		return nil, errEgress
	}
	if resp == nil || resp.Body == nil {
		cancel()
		return nil, errEgress
	}
	if resp.ContentLength > e.maxBytes {
		cancel()
		resp.Body.Close()
		return nil, errEgressSize
	}
	resp.Body = &egressBody{ReadCloser: resp.Body, remaining: e.maxBytes, cancel: cancel}
	return resp, nil
}

type egressBody struct {
	io.ReadCloser
	remaining int64
	cancel    context.CancelFunc
}

func (b *egressBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if b.remaining == 0 {
		var probe [1]byte
		n, err := b.ReadCloser.Read(probe[:])
		if n != 0 {
			b.cancel()
			return 0, errEgressSize
		}
		if err != nil && err != io.EOF {
			return 0, errEgress
		}
		return 0, err
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	if err != nil && err != io.EOF {
		return n, errEgress
	}
	return n, err
}
func (b *egressBody) Close() error { b.cancel(); return b.ReadCloser.Close() }

// The host dispatch retains the account's existing TLS profile, connection
// limits, generation permit and billing path. Forwards never replay on redirect.
func controlledProvider(req *http.Request, send func(*http.Request) (*http.Response, error)) (*http.Response, error) {
	e := ControlledEgress{timeout: DefaultGenerationDeadline, maxBytes: 64 << 20}
	return e.send(req.WithContext(WithHTTPUpstreamRedirectsDisabled(req.Context())), send)
}
