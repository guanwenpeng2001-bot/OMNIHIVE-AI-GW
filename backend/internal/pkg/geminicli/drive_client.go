package geminicli

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/httpclient"
)

// DriveStorageInfo represents Google Drive storage quota information
type DriveStorageInfo struct {
	Limit int64 `json:"limit"` // Storage limit in bytes
	Usage int64 `json:"usage"` // Current usage in bytes
}

// DriveClient interface for Google Drive API operations
type DriveClient interface {
	GetStorageQuota(ctx context.Context, accessToken, proxyURL string) (*DriveStorageInfo, error)
}

type driveClient struct{}

// NewDriveClient creates a new Drive API client
func NewDriveClient() DriveClient {
	return &driveClient{}
}

// GetStorageQuota fetches storage quota from Google Drive API
func (c *driveClient) GetStorageQuota(ctx context.Context, accessToken, proxyURL string) (*DriveStorageInfo, error) {
	const driveAPIURL = "https://www.googleapis.com/drive/v3/about?fields=storageQuota"

	req, err := http.NewRequestWithContext(ctx, "GET", driveAPIURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)

	// Get HTTP client with proxy support
	client, err := httpclient.GetClient(httpclient.Options{
		ProxyURL: proxyURL,
		Timeout:  10 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	resp, err := doDriveRequest(client, req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		statusText := http.StatusText(resp.StatusCode)
		if statusText == "" {
			statusText = resp.Status
		}
		fmt.Printf("[DriveClient] Drive API error: status=%d, msg=%s\n", resp.StatusCode, statusText)
		// 只返回通用错误
		return nil, fmt.Errorf("drive API error: status %d", resp.StatusCode)
	}

	defer func() { _ = resp.Body.Close() }()

	// Parse response
	var result struct {
		StorageQuota struct {
			Limit string `json:"limit"` // Can be string or number
			Usage string `json:"usage"`
		} `json:"storageQuota"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Parse limit and usage (handle both string and number formats)
	var limit, usage int64
	if result.StorageQuota.Limit != "" {
		if val, err := strconv.ParseInt(result.StorageQuota.Limit, 10, 64); err == nil {
			limit = val
		}
	}
	if result.StorageQuota.Usage != "" {
		if val, err := strconv.ParseInt(result.StorageQuota.Usage, 10, 64); err == nil {
			usage = val
		}
	}

	return &DriveStorageInfo{
		Limit: limit,
		Usage: usage,
	}, nil
}

func doDriveRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	sleepWithContext := func(d time.Duration) error {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	}

	const maxAttempts = 3
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	for attempt := 1; ; attempt++ {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("request cancelled: %w", ctx.Err())
		}
		resp, err := client.Do(req)
		if err != nil {
			if attempt == maxAttempts {
				return nil, fmt.Errorf("network error after %d attempts: %w", maxAttempts, err)
			}
		} else {
			switch resp.StatusCode {
			case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable:
				if attempt == maxAttempts {
					return resp, nil
				}
				_ = resp.Body.Close()
			default:
				return resp, nil
			}
		}
		backoff := time.Duration(1<<uint(attempt-1)) * time.Second
		jitter := time.Duration(rng.Intn(1000)) * time.Millisecond
		if err := sleepWithContext(backoff + jitter); err != nil {
			return nil, fmt.Errorf("request cancelled: %w", err)
		}
	}
}
