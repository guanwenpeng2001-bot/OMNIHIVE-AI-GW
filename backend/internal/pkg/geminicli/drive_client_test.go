package geminicli

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDriveStorageInfo(t *testing.T) {
	// 测试 DriveStorageInfo 结构体
	info := &DriveStorageInfo{
		Limit: 100 * 1024 * 1024 * 1024, // 100GB
		Usage: 50 * 1024 * 1024 * 1024,  // 50GB
	}

	if info.Limit != 100*1024*1024*1024 {
		t.Errorf("Expected limit 100GB, got %d", info.Limit)
	}
	if info.Usage != 50*1024*1024*1024 {
		t.Errorf("Expected usage 50GB, got %d", info.Usage)
	}
}

type driveTransportFunc func(*http.Request) (*http.Response, error)

func (f driveTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type driveRetryBody struct {
	io.Reader
	onClose func()
}

func (b driveRetryBody) Close() error { b.onClose(); return nil }

func TestDriveClosesRetryBodyBeforeBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.test", nil)
		require.NoError(t, err)
		start := time.Now()
		client := &http.Client{Transport: driveTransportFunc(func(*http.Request) (*http.Response, error) {
			body := driveRetryBody{strings.NewReader("busy"), cancel}
			return &http.Response{StatusCode: http.StatusTooManyRequests, Body: body}, nil
		})}
		_, err = doDriveRequest(client, req)
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, start, time.Now(), "closing the response should cancel the pending backoff immediately")
	})
}
