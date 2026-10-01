//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQianwenVideoAcceleratedOrigin(t *testing.T) {
	original := qianwenVideoContentTransport
	t.Cleanup(func() { qianwenVideoContentTransport = original })
	calls := 0
	qianwenVideoContentTransport = qianwenSpeechRoundTripper(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "https", request.URL.Scheme)
		require.Empty(t, request.Header.Get("Authorization"))
		require.Empty(t, request.Header.Get("Cookie"))
		require.Empty(t, request.Header.Get("Proxy-Authorization"))
		return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: 5, Body: io.NopCloser(strings.NewReader("video"))}, nil
	})
	for _, host := range []string{"dashscope-result-bj.oss-cn-beijing.aliyuncs.com", "dashscope-result-sh.oss-accelerate.aliyuncs.com", "dashscope-a717.oss-accelerate.aliyuncs.com", "dashscope-other-bucket.oss-accelerate.aliyuncs.com", "dashscope-a717.oss-cn-beijing.aliyuncs.com", "dashscope-other-bucket.oss-cn-hangzhou.aliyuncs.com"} {
		t.Run(host, func(t *testing.T) {
			response, err := OpenQianwenVideoContent(context.Background(), "https://"+host+"/synthetic.mp4?Expires=123&Signature=synthetic")
			require.NoError(t, err)
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, "video", string(body))
		})
	}
	require.Equal(t, 6, calls)
}

func TestQianwenVideoRejectsUntrustedOriginsBeforeHTTP(t *testing.T) {
	original := qianwenVideoContentTransport
	t.Cleanup(func() { qianwenVideoContentTransport = original })
	qianwenVideoContentTransport = qianwenSpeechRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid origin reached HTTP")
		return nil, nil
	})
	for _, address := range []string{
		"http://dashscope-a717.oss-accelerate.aliyuncs.com/video.mp4",
		"https://dashscope-a717.oss-accelerate.aliyuncs.com.evil.test/video.mp4",
		"https://dashscope-a717.evil.oss-accelerate.aliyuncs.com/video.mp4",
		"https://attacker.oss-accelerate.aliyuncs.com/video.mp4",
		"https://dashscope-a717.aliyuncs.com/video.mp4",
		"https://dashscope-a717.oss-cn-beijing-internal.aliyuncs.com/video.mp4",
		"https://dashscope-a717.oss-cn-.aliyuncs.com/video.mp4",
		"https://dashscope-a717.oss-cn--beijing.aliyuncs.com/video.mp4",
		"https://dashscope-a717.oss-cn-beijing1.aliyuncs.com/video.mp4",
		"https://dashscope-a717.oss-cn-beijing.aliyuncs.com.evil.test/video.mp4",
		"https://dashscope-a717.evil.oss-cn-beijing.aliyuncs.com/video.mp4",
		"https://dashscope-a717.oss-ap-southeast-1.aliyuncs.com/video.mp4",
		"https://other-bucket.oss-cn-beijing.aliyuncs.com/video.mp4",
		"https://dashscope-a717.oss-cn-" + strings.Repeat("a", 57) + ".aliyuncs.com/video.mp4",
		"https://dashscope-.oss-accelerate.aliyuncs.com/video.mp4",
		"https://dashscope--a.oss-accelerate.aliyuncs.com/video.mp4",
		"https://dashscope-a_.oss-accelerate.aliyuncs.com/video.mp4",
		"https://dashscope-a-.oss-accelerate.aliyuncs.com/video.mp4",
		"https://dashscope-" + strings.Repeat("a", 55) + ".oss-accelerate.aliyuncs.com/video.mp4",
		"https://user:password@dashscope-a717.oss-accelerate.aliyuncs.com/video.mp4",
		"https://dashscope-a717.oss-accelerate.aliyuncs.com:443/video.mp4",
		"https://dashscope-a717.oss-accelerate.aliyuncs.com:8443/video.mp4",
		"https://dashscope-a717.oss-accelerate.aliyuncs.com:/video.mp4",
		"https://dashscope-a717.oss-accelerate.aliyuncs.com/video.mp4#fragment",
		"https://dashscope-a717.oss-accelerate.aliyuncs.com/video.mp4#",
		"https://127.0.0.1/video.mp4",
		"https://[::1]/video.mp4",
		"https://47.113.75.27/video.mp4",
		"https://example.com/video.mp4",
	} {
		_, err := OpenQianwenVideoContent(context.Background(), address)
		require.ErrorContains(t, err, "origin", address)
	}
}

type qianwenVideoDownloadBody struct {
	*strings.Reader
	closed bool
}

func (body *qianwenVideoDownloadBody) Close() error { body.closed = true; return nil }

func TestQianwenVideoDownloadSizeAndClose(t *testing.T) {
	original := qianwenVideoContentTransport
	t.Cleanup(func() { qianwenVideoContentTransport = original })
	for _, length := range []int64{-1, 1, 256 << 20, (256 << 20) + 1} {
		body := &qianwenVideoDownloadBody{Reader: strings.NewReader("video")}
		qianwenVideoContentTransport = qianwenSpeechRoundTripper(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: length, Body: body}, nil
		})
		response, err := OpenQianwenVideoContent(context.Background(), "https://dashscope-a717.oss-accelerate.aliyuncs.com/synthetic.mp4")
		if length < 0 || length > 256<<20 {
			require.ErrorContains(t, err, "limit")
			require.True(t, body.closed)
			require.Equal(t, 5, body.Len(), "rejected bodies are never consumed")
			continue
		}
		require.NoError(t, err)
		data, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		if length == 1 {
			require.Equal(t, "v", string(data), "declared length caps reads even if the body is longer")
			require.Equal(t, 4, body.Len())
		} else {
			require.Equal(t, "video", string(data))
		}
		require.NoError(t, response.Body.Close())
		require.True(t, body.closed, "bounded wrapper retains the upstream closer")
	}
}

func TestQianwenVideoDownloadNeverFollowsRedirects(t *testing.T) {
	original := qianwenVideoContentTransport
	t.Cleanup(func() { qianwenVideoContentTransport = original })
	for _, location := range []string{"http://127.0.0.1/private", "https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/other.mp4"} {
		calls := 0
		body := &qianwenVideoDownloadBody{Reader: strings.NewReader("redirect")}
		qianwenVideoContentTransport = qianwenSpeechRoundTripper(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{location}}, Body: body}, nil
		})
		_, err := OpenQianwenVideoContent(context.Background(), "https://dashscope-a717.oss-accelerate.aliyuncs.com/synthetic.mp4")
		require.ErrorContains(t, err, "HTTP 302")
		require.Equal(t, 1, calls)
		require.True(t, body.closed)
	}
}
