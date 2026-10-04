package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// QianwenVideoCompatBody converts the standard text-to-video fields without
// guessing a different model, duration or tariff. Image references require a
// separate upload contract and are rejected rather than silently discarded.
func QianwenVideoCompatBody(body []byte) ([]byte, error) {
	canonical, err := CanonicalUpstreamJobBody(body)
	if err != nil {
		return nil, err
	}
	request := gjson.ParseBytes(canonical)
	if !request.IsObject() {
		return nil, errors.New("video request must be an object")
	}
	for field := range request.Map() {
		switch field {
		case "model", "prompt", "seconds", "size":
		default:
			return nil, fmt.Errorf("unsupported video field: %s", field)
		}
	}
	model, prompt := request.Get("model"), request.Get("prompt")
	if model.Type != gjson.String || strings.TrimSpace(model.String()) == "" || prompt.Type != gjson.String || strings.TrimSpace(prompt.String()) == "" {
		return nil, errors.New("model and prompt are required strings")
	}
	seconds := 5
	if value := request.Get("seconds"); value.Exists() {
		if value.Type != gjson.String && value.Type != gjson.Number {
			return nil, errors.New("seconds must be a positive integer")
		}
		seconds, err = strconv.Atoi(value.String())
		if err != nil || seconds <= 0 {
			return nil, errors.New("seconds must be a positive integer")
		}
	}
	size := "1280x720"
	if value := request.Get("size"); value.Exists() {
		if value.Type != gjson.String {
			return nil, errors.New("size must be WIDTHxHEIGHT")
		}
		size = value.String()
	}
	parts := strings.Split(size, "x")
	if len(parts) != 2 {
		return nil, errors.New("size must be WIDTHxHEIGHT")
	}
	nativeSize := strings.Join(parts, "*")
	if videoUsageResolution(nativeSize) == "" {
		return nil, errors.New("unsupported video size")
	}
	return json.Marshal(map[string]any{"model": model.String(), "input": map[string]any{"prompt": prompt.String()}, "parameters": map[string]any{"duration": seconds, "size": nativeSize}})
}

func QianwenVideoCompatStatus(status string) string {
	switch status {
	case "submitted":
		return "queued"
	case "running":
		return "in_progress"
	case "succeeded":
		return "completed"
	default:
		return "failed"
	}
}

var ErrQianwenVideoNotCancelable = errors.New("only queued upstream video tasks can be canceled")

// CancelQianwenVideo calls the documented DashScope PENDING-only operation.
// Acknowledgement is not a durable terminal state: the existing poller must
// still observe the actual outcome and settle a racing success normally.
// https://help.aliyun.com/en/model-studio/manage-asynchronous-tasks
func (s *OpenAIGatewayService) CancelQianwenVideo(ctx context.Context, job *UpstreamJob) (int, error) {
	if job == nil || job.Platform != PlatformQianwen || job.Family != "video" || job.Status != "submitted" || job.UpstreamTaskID == "" {
		return http.StatusConflict, ErrQianwenVideoNotCancelable
	}
	account, err := s.QianwenResourceAccount(ctx, job, false)
	if err != nil {
		return http.StatusServiceUnavailable, errors.New("video account unavailable")
	}
	if strings.TrimSpace(account.GetCNAPIKey()) == "" {
		return http.StatusServiceUnavailable, errors.New("video account unavailable")
	}
	base := DefaultQianwenDashScopeBaseURL
	if bases, ok := account.Credentials["api_base_urls"].(map[string]any); ok {
		if override, ok := bases["dashscope"].(string); ok && strings.TrimSpace(override) != "" {
			base = strings.TrimSpace(override)
		}
	}
	base, err = s.validateUpstreamBaseURL(base)
	if err != nil {
		return http.StatusServiceUnavailable, errors.New("video endpoint unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/tasks/"+url.PathEscape(job.UpstreamTaskID)+"/cancel", nil)
	if err != nil {
		return http.StatusServiceUnavailable, errors.New("video endpoint unavailable")
	}
	account.ApplyHeaderOverrides(req.Header)
	req.Header.Set("Authorization", "Bearer "+account.GetCNAPIKey())
	req.Header.Set("Accept", "application/json")
	req.Header.Del("X-DashScope-Async")
	req.Header.Del("X-DashScope-SSE")
	accountProxyURL, egressErr := account.EgressProxyURL(ctx)
	if egressErr != nil {
		return 0, egressErr
	}
	resp, err := s.doOpenAIUpstream(req, accountProxyURL, account)
	if err != nil {
		return http.StatusBadGateway, errors.New("video cancellation outcome unknown; polling continues")
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Debug("provider response cleanup failed")
		}
	}()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 {
		return http.StatusBadGateway, errors.New("video cancellation outcome unknown; polling continues")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusBadRequest && gjson.GetBytes(raw, "code").String() == "UnsupportedOperation" {
			return http.StatusConflict, ErrQianwenVideoNotCancelable
		}
		return resp.StatusCode, fmt.Errorf("DashScope rejected video cancellation (HTTP %d); polling continues", resp.StatusCode)
	}
	var requestID string
	if json.Unmarshal(raw, &requestID) != nil || strings.TrimSpace(requestID) == "" {
		return http.StatusBadGateway, errors.New("unrecognized cancellation acknowledgement; polling continues")
	}
	return http.StatusOK, nil
}
