package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/stretchr/testify/require"
)

func TestFinalizeProxyQualityResult_ScoreAndGrade(t *testing.T) {
	result := &ProxyQualityCheckResult{
		PassedCount:    2,
		WarnCount:      1,
		FailedCount:    1,
		ChallengeCount: 1,
	}

	finalizeProxyQualityResult(result)

	require.Equal(t, 38, result.Score)
	require.Equal(t, "F", result.Grade)
	require.Contains(t, result.Summary, "通过 2 项")
	require.Contains(t, result.Summary, "告警 1 项")
	require.Contains(t, result.Summary, "失败 1 项")
	require.Contains(t, result.Summary, "挑战 1 项")
}

func TestRunProxyQualityTarget_CloudflareChallenge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("cf-ray", "test-ray-123")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<!DOCTYPE html><title>Just a moment...</title><script>window._cf_chl_opt={};</script>"))
	}))
	defer server.Close()

	target := proxyQualityTarget{
		Target: "openai",
		URL:    server.URL,
		Method: http.MethodGet,
		AllowedStatuses: map[int]struct{}{
			http.StatusUnauthorized: {},
		},
	}

	item := runProxyQualityTarget(context.Background(), server.Client(), target)
	require.Equal(t, "challenge", item.Status)
	require.Equal(t, http.StatusForbidden, item.HTTPStatus)
	require.Equal(t, "test-ray-123", item.CFRay)
}

func TestRunProxyQualityTarget_AllowedStatusPass(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer server.Close()

	target := proxyQualityTarget{
		Target: "gemini",
		URL:    server.URL,
		Method: http.MethodGet,
		AllowedStatuses: map[int]struct{}{
			http.StatusOK: {},
		},
	}

	item := runProxyQualityTarget(context.Background(), server.Client(), target)
	require.Equal(t, "pass", item.Status)
	require.Equal(t, http.StatusOK, item.HTTPStatus)
}

func TestRunProxyQualityTarget_AllowedStatusPassForUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer server.Close()

	target := proxyQualityTarget{
		Target: "openai",
		URL:    server.URL,
		Method: http.MethodGet,
		AllowedStatuses: map[int]struct{}{
			http.StatusUnauthorized: {},
		},
	}

	item := runProxyQualityTarget(context.Background(), server.Client(), target)
	require.Equal(t, "pass", item.Status)
	require.Equal(t, http.StatusUnauthorized, item.HTTPStatus)
	require.Contains(t, item.Message, "目标可达")
}

func TestProxyQualityTargets_IncludesGrok(t *testing.T) {
	var grokTarget *proxyQualityTarget
	for i := range proxyQualityTargets {
		if proxyQualityTargets[i].Target == "grok" {
			grokTarget = &proxyQualityTargets[i]
			break
		}
	}

	require.NotNil(t, grokTarget)
	require.Equal(t, "https://api.x.ai/v1/models", grokTarget.URL)
	require.Equal(t, http.MethodGet, grokTarget.Method)
	require.Contains(t, grokTarget.AllowedStatuses, http.StatusUnauthorized)
}

func TestRunProxyQualityTarget_GrokUnauthorizedPasses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer server.Close()

	target := proxyQualityTarget{
		Target: "grok",
		URL:    server.URL,
		Method: http.MethodGet,
		AllowedStatuses: map[int]struct{}{
			http.StatusUnauthorized: {},
		},
	}

	item := runProxyQualityTarget(context.Background(), server.Client(), target)
	require.Equal(t, "grok", item.Target)
	require.Equal(t, "pass", item.Status)
	require.Equal(t, http.StatusUnauthorized, item.HTTPStatus)
	require.Contains(t, item.Message, "目标可达")
}

// Unexpected repository operations panic through the embedded interface.
type qualityProxyRepository struct {
	ProxyRepository
	proxy *Proxy
}

func (r *qualityProxyRepository) GetByID(context.Context, int64) (*Proxy, error) { return r.proxy, nil }

type qualityMemoryCache struct{ snapshot *ProxyLatencyInfo }

func (c *qualityMemoryCache) GetProxyLatencies(context.Context, []int64) (map[int64]*ProxyLatencyInfo, error) {
	return map[int64]*ProxyLatencyInfo{1: c.snapshot}, nil
}
func (c *qualityMemoryCache) SetProxyLatency(_ context.Context, _ int64, info *ProxyLatencyInfo) error {
	c.snapshot = info
	return nil
}

type qualityFakeProber struct {
	err   error
	calls int
}

func (p *qualityFakeProber) ProbeProxy(context.Context, string) (*ProxyExitInfo, int64, error) {
	p.calls++
	if p.err != nil {
		return nil, 17, p.err
	}
	return &ProxyExitInfo{IP: "192.0.2.1", Country: "Test"}, 23, nil
}

type qualityRoundTripper func(*http.Request) (*http.Response, error)

func (f qualityRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestProxyQualityCheck_BaseFailureStillChecksEveryProvider(t *testing.T) {
	for index, tc := range []struct {
		name      string
		baseFails bool
		response  string
		status    string
		assessed  bool
	}{
		{"domain restricted but providers reachable", true, "allowed", "warn", true},
		{"all targets fail", true, "error", "failed", false},
		{"provider rate limited", true, "rate", "warn", true},
		{"challenge is not healthy", true, "challenge", "challenge", false},
		{"all reachable baseline", false, "allowed", "healthy", true},
		{"base only reachable baseline", false, "error", "failed", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proxy := &Proxy{ID: 1, Protocol: "http", Host: "quality-test.invalid", Port: 12000 + index, Status: StatusActive}
			before := *proxy
			prober := &qualityFakeProber{}
			if tc.baseFails {
				prober.err = errors.New("IP echo unavailable")
			}
			cache := &qualityMemoryCache{}
			svc := &adminServiceImpl{proxyRepo: &qualityProxyRepository{proxy: proxy}, proxyProber: prober, proxyLatencyCache: cache}
			// Replace the exact production client's transport: no DNS, proxy or upstream traffic.
			client, err := httpclient.GetClient(httpclient.Options{ProxyURL: proxy.URL(), Timeout: proxyQualityRequestTimeout, ResponseHeaderTimeout: proxyQualityResponseHeaderTimeout})
			require.NoError(t, err)
			original := client.Transport
			t.Cleanup(func() { client.Transport = original })
			var urls []string
			client.Transport = qualityRoundTripper(func(req *http.Request) (*http.Response, error) {
				i := len(urls)
				urls = append(urls, req.URL.String())
				require.Equal(t, proxyQualityTargets[i].Method, req.Method)
				if tc.response == "error" {
					return nil, errors.New("synthetic tunnel EOF")
				}
				status := []int{401, 405, 200, 401}[i]
				body := "{}"
				headers := make(http.Header)
				if tc.response == "rate" {
					status = 429
				}
				if tc.response == "challenge" {
					status = 403
					headers.Set("cf-ray", "synthetic-ray")
					body = "<title>Just a moment...</title><script>window._cf_chl_opt={};</script>"
				}
				return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})
			result, err := svc.CheckProxyQuality(context.Background(), 1)
			require.NoError(t, err)
			require.Equal(t, 1, prober.calls)
			require.Len(t, result.Items, 5)
			require.Len(t, urls, 4)
			for i, target := range proxyQualityTargets {
				require.Equal(t, target.URL, urls[i])
				require.Equal(t, target.Target, result.Items[i+1].Target)
			}
			require.Equal(t, tc.status, proxyQualityOverallStatus(result))
			require.Equal(t, tc.status, cache.snapshot.QualityStatus)
			require.NotNil(t, cache.snapshot.QualityCheckedAt)
			require.Equal(t, result.CheckedAt, *cache.snapshot.QualityCheckedAt)
			require.Equal(t, !tc.baseFails, cache.snapshot.Success)
			require.Equal(t, before, *proxy)
			if tc.baseFails {
				require.Nil(t, cache.snapshot.LatencyMs)
				require.Empty(t, result.ExitIP)
			}
			if tc.assessed {
				require.NotNil(t, cache.snapshot.QualityScore)
				require.Equal(t, result.Score, *cache.snapshot.QualityScore)
				require.NotEmpty(t, result.Grade)
			} else {
				require.Nil(t, cache.snapshot.QualityScore)
				require.Empty(t, cache.snapshot.QualityGrade)
				require.Empty(t, result.Grade)
				require.Zero(t, result.Score)
			}
			if tc.response == "allowed" && tc.baseFails {
				require.Equal(t, 78, result.Score)
				require.Equal(t, "B", result.Grade)
			}
		})
	}
}

func TestProxyQualityCheck_Incomplete(t *testing.T) {
	for _, name := range []string{"no prober", "client error with base failure", "client error with base success"} {
		t.Run(name, func(t *testing.T) {
			proxy := &Proxy{ID: 1, Protocol: "unsupported", Host: "quality-test.invalid", Port: 1}
			cache := &qualityMemoryCache{}
			svc := &adminServiceImpl{proxyRepo: &qualityProxyRepository{proxy: proxy}, proxyLatencyCache: cache}
			if name != "no prober" {
				prober := &qualityFakeProber{}
				if name == "client error with base failure" {
					prober.err = errors.New("echo unavailable")
				}
				svc.proxyProber = prober
			}
			result, err := svc.CheckProxyQuality(context.Background(), 1)
			require.NoError(t, err)
			require.NotNil(t, cache.snapshot.QualityCheckedAt)
			require.Equal(t, "failed", cache.snapshot.QualityStatus)
			if name == "no prober" {
				require.Len(t, result.Items, 1)
			} else {
				require.Len(t, result.Items, 2)
				require.Equal(t, "http_client", result.Items[1].Target)
			}
			if name != "client error with base success" {
				require.Empty(t, result.Grade)
				require.Zero(t, result.Score)
				require.Nil(t, cache.snapshot.QualityScore)
			}
		})
	}
}

func TestProxyQualityTarget_ProviderSpecificAllowedStatuses(t *testing.T) {
	for _, target := range proxyQualityTargets {
		for status := range target.AllowedStatuses {
			client := &http.Client{Transport: qualityRoundTripper(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}")), Request: req}, nil
			})}
			item := runProxyQualityTarget(context.Background(), client, target)
			require.Equal(t, "pass", item.Status, "%s HTTP %d", target.Target, status)
		}
	}
}

func TestProxyLatency_FailurePreservesDatedQualityOnlyUntilNextQualityCheck(t *testing.T) {
	score, checkedAt := 100, int64(123)
	cache := &qualityMemoryCache{snapshot: &ProxyLatencyInfo{QualityScore: &score, QualityGrade: "A", QualityCheckedAt: &checkedAt, QualityStatus: "healthy"}}
	svc := &adminServiceImpl{proxyLatencyCache: cache}
	svc.saveProxyLatency(context.Background(), 1, &ProxyLatencyInfo{Success: false, UpdatedAt: time.Now()})
	require.False(t, cache.snapshot.Success)
	require.Equal(t, checkedAt, *cache.snapshot.QualityCheckedAt)
	require.Equal(t, score, *cache.snapshot.QualityScore)
	result := &ProxyQualityCheckResult{FailedCount: 5, CheckedAt: 456}
	finalizeProxyQualityResult(result)
	svc.saveProxyQualitySnapshot(context.Background(), 1, result, nil)
	require.Nil(t, cache.snapshot.QualityScore)
	require.Empty(t, cache.snapshot.QualityGrade)
	require.Equal(t, int64(456), *cache.snapshot.QualityCheckedAt)
}
