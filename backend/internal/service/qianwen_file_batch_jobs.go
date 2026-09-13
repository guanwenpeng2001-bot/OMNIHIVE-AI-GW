package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tidwall/gjson"
)

const QianwenFileLimit = 1 << 20
const QianwenBatchTestModel = "batch-test-model"

// Only the documented free diagnostic model is admitted until file-batch price
// cards exist. Synchronous Batch Chat prices must never price file jobs.
type QianwenFileBatchSnapshot struct {
	SHA256         string   `json:"sha256,omitempty"`
	Bytes          int      `json:"bytes,omitempty"`
	CustomIDHashes []string `json:"custom_id_hashes"`
	InputFileID    string   `json:"input_file_id,omitempty"`
}

func fileBatchHash(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

// Normalize before hashing AND forwarding; multipart boundaries are irrelevant
// to idempotency. Persist only hashes so prompts/custom IDs stay out of the DB.
func ValidateQianwenBatchFile(raw []byte) ([]byte, *QianwenFileBatchSnapshot, error) {
	if len(raw) == 0 || len(raw) > QianwenFileLimit || !utf8.Valid(raw) {
		return nil, nil, errors.New("batch test files must be UTF-8 JSONL up to 1 MiB")
	}
	var normalized bytes.Buffer
	snapshot := &QianwenFileBatchSnapshot{}
	seen := map[string]bool{}
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		canonical, err := CanonicalUpstreamJobBody(line)
		if err != nil {
			return nil, nil, errors.New("each batch line must be JSON")
		}
		row := gjson.ParseBytes(canonical)
		id := row.Get("custom_id")
		if !row.IsObject() || id.Type != gjson.String || id.String() == "" || len(id.String()) > 256 || seen[id.String()] || len(seen) >= 100 {
			return nil, nil, errors.New("batch requires 1-100 unique custom_id values")
		}
		if row.Get("method").String() != "POST" || row.Get("url").String() != "/v1/chat/ds-test" || row.Get("body.model").String() != QianwenBatchTestModel || row.Get("body.stream").Bool() || !row.Get("body.messages").IsArray() {
			return nil, nil, errors.New("only non-streaming batch-test-model at /v1/chat/ds-test is supported; paid file-batch tariffs are unavailable")
		}
		seen[id.String()] = true
		snapshot.CustomIDHashes = append(snapshot.CustomIDHashes, fileBatchHash([]byte(id.String())))
		normalized.Write(canonical)
		normalized.WriteByte('\n')
	}
	if normalized.Len() > QianwenFileLimit {
		return nil, nil, errors.New("normalized file exceeds 1 MiB")
	}
	snapshot.SHA256 = fileBatchHash(normalized.Bytes())
	snapshot.Bytes = normalized.Len()
	return normalized.Bytes(), snapshot, nil
}

func QianwenJobCharge(j *UpstreamJob) (*UsageLog, *UsageBillingCommand, error) {
	if j.Family != "files" && j.Family != "batch" {
		return QianwenVideoJobCharge(j)
	}
	if j.Platform != PlatformQianwen || j.Billing.Model != QianwenBatchTestModel || (j.Billing.FileBatch == nil || len(j.Billing.FileBatch.CustomIDHashes) == 0) {
		return nil, nil, ErrUpstreamJobUnsupportedUsage
	}
	var input, output int
	if j.Family == "files" {
		if j.UpstreamTaskID == "" || j.ResultRef != j.UpstreamTaskID || j.Billing.FileBatch.SHA256 == "" {
			return nil, nil, ErrUpstreamJobUnsupportedUsage
		}
	} else {
		usage := gjson.ParseBytes(j.UsageJSON)
		if j.ResultRef == "" || usage.Get("validated_items").Int() != int64(len(j.Billing.FileBatch.CustomIDHashes)) || !usage.Get("free_test_model").Bool() {
			return nil, nil, ErrUpstreamJobUnsupportedUsage
		}
		input = int(usage.Get("prompt_tokens").Int())
		output = int(usage.Get("completion_tokens").Int())
	}
	mode, source, version := "file_batch", "dashscope_free_test_model", "file-batch-test-v1"
	log := &UsageLog{UserID: j.UserID, APIKeyID: j.APIKeyID, AccountID: j.AccountID, GroupID: j.GroupID, RequestID: j.PublicRequestID, Model: j.Billing.Model, RequestedModel: j.Billing.RequestedModel, InputTokens: input, OutputTokens: output, RateMultiplier: 1, BillingMode: &mode, PriceSource: &source, PriceCardVersion: &version, RequestType: RequestTypeSync, CreatedAt: time.Now().UTC()}
	cmd := &UsageBillingCommand{RequestID: j.PublicRequestID, RequestFingerprint: j.RequestFingerprint, UserID: j.UserID, APIKeyID: j.APIKeyID, AccountID: j.AccountID, AccountType: AccountTypeAPIKey, Model: j.Billing.Model}
	return log, cmd, nil
}

func (s *OpenAIGatewayService) QianwenResourceAccount(ctx context.Context, j *UpstreamJob, submit bool) (*Account, error) {
	a, err := s.accountRepo.GetByID(ctx, j.AccountID)
	if err != nil {
		return nil, err
	}
	if a.Platform != PlatformQianwen || a.Type != AccountTypeAPIKey || !a.IsActive() {
		return nil, errors.New("resource account unavailable")
	}
	if submit && (!a.IsSchedulable() || j.GroupID == nil || !slices.Contains(a.GroupIDs, *j.GroupID)) {
		return nil, errors.New("resource account no longer belongs to the active group or is unschedulable")
	}
	return a, nil
}

// Only fixed resource paths built from opaque IDs reach this transport. Provider
// content URLs are never followed; the existing validated base/proxy is reused.
func (s *OpenAIGatewayService) qianwenResourceRequest(ctx context.Context, a *Account, method, path, contentType string, body []byte) ([]byte, int, error) {
	if a == nil || a.Platform != PlatformQianwen || a.Type != AccountTypeAPIKey || a.GetCNAPIKey() == "" {
		return nil, 0, errors.New("Qianwen API-key account required")
	}
	base := a.GetCNProtocolBaseURL(APIProtocolChatCompletions)
	if base == "" {
		base = DefaultQianwenBaseURL
	}
	base, err := s.validateUpstreamBaseURL(base)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	a.ApplyHeaderOverrides(req.Header)
	req.Header.Set("Authorization", "Bearer "+a.GetCNAPIKey())
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	req.Header.Del("X-DashScope-Async")
	req.Header.Del("X-DashScope-SSE")
	resp, err := s.doOpenAIUpstream(req, upstreamModelsProxyURL(a), a)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4*QianwenFileLimit+1))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if len(raw) > 4*QianwenFileLimit {
		return nil, resp.StatusCode, errors.New("resource response exceeds 4 MiB")
	}
	if resp.StatusCode >= 500 || resp.StatusCode == 429 {
		return nil, resp.StatusCode, errors.New("resource upstream temporarily unavailable")
	}
	return raw, resp.StatusCode, nil
}
func resourceFailure(status int) UpstreamJobUpdate {
	return UpstreamJobUpdate{Status: "failed", ErrorCode: "upstream_rejected", ErrorMessage: fmt.Sprintf("DashScope rejected resource request (HTTP %d)", status)}
}
func (s *OpenAIGatewayService) SubmitQianwenFile(ctx context.Context, a *Account, body []byte) (UpstreamJobUpdate, error) {
	var form bytes.Buffer
	w := multipart.NewWriter(&form)
	_ = w.WriteField("purpose", "batch")
	part, err := w.CreateFormFile("file", "batch-input.jsonl")
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	if _, err = part.Write(body); err != nil {
		return UpstreamJobUpdate{}, err
	}
	if err = w.Close(); err != nil {
		return UpstreamJobUpdate{}, err
	}
	raw, status, err := s.qianwenResourceRequest(ctx, a, http.MethodPost, "/files", w.FormDataContentType(), form.Bytes())
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	if status < 200 || status >= 300 {
		return resourceFailure(status), nil
	}
	id := gjson.GetBytes(raw, "id").String()
	if !json.Valid(raw) || id == "" || gjson.GetBytes(raw, "purpose").String() != "batch" || gjson.GetBytes(raw, "status").String() != "processed" {
		return UpstreamJobUpdate{}, errors.New("invalid file upload response")
	}
	return UpstreamJobUpdate{TaskID: id, Status: "succeeded", ResultRef: id}, nil
}
func (s *OpenAIGatewayService) SubmitQianwenBatch(ctx context.Context, a *Account, file *UpstreamJob) (UpstreamJobUpdate, error) {
	rawBody, _ := json.Marshal(map[string]string{"input_file_id": file.UpstreamTaskID, "endpoint": "/v1/chat/ds-test", "completion_window": "24h"})
	raw, status, err := s.qianwenResourceRequest(ctx, a, http.MethodPost, "/batches", "application/json", rawBody)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	if status < 200 || status >= 300 {
		return resourceFailure(status), nil
	}
	// Even immediate completion is polled to validate the result file before settlement.
	u, err := parseQianwenBatch(raw)
	if u.Status == "succeeded" {
		u.Status = "running"
	}
	return u, err
}
func parseQianwenBatch(raw []byte) (UpstreamJobUpdate, error) {
	if !json.Valid(raw) {
		return UpstreamJobUpdate{}, errors.New("invalid batch JSON")
	}
	u := UpstreamJobUpdate{TaskID: gjson.GetBytes(raw, "id").String(), ResultRef: gjson.GetBytes(raw, "output_file_id").String()}
	if u.TaskID == "" {
		return u, errors.New("missing batch identity")
	}
	switch gjson.GetBytes(raw, "status").String() {
	case "validating":
		u.Status = "submitted"
	case "in_progress", "finalizing", "cancelling":
		u.Status = "running"
	case "completed":
		u.Status = "succeeded"
	case "failed", "cancelled":
		u.Status = "failed"
		u.ErrorCode = "upstream_failed"
		u.ErrorMessage = "DashScope batch failed or was cancelled"
	case "expired":
		u.Status = "expired"
	default:
		u.Status = "unsupported"
	}
	return u, nil
}
func (s *OpenAIGatewayService) PollQianwenJob(ctx context.Context, j *UpstreamJob) (UpstreamJobUpdate, error) {
	if j.Family != "batch" {
		return s.PollQianwenVideo(ctx, j)
	}
	a, err := s.QianwenResourceAccount(ctx, j, false)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	raw, status, err := s.qianwenResourceRequest(ctx, a, http.MethodGet, "/batches/"+url.PathEscape(j.UpstreamTaskID), "application/json", nil)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	if status < 200 || status >= 300 {
		return resourceFailure(status), nil
	}
	u, err := parseQianwenBatch(raw)
	if err != nil || u.Status != "succeeded" || u.TaskID != j.UpstreamTaskID {
		return u, err
	}
	counts := gjson.GetBytes(raw, "request_counts")
	if j.Billing.FileBatch == nil || counts.Get("failed").Int() != 0 || counts.Get("completed").Int() != int64(len(j.Billing.FileBatch.CustomIDHashes)) || counts.Get("total").Int() != counts.Get("completed").Int() || u.ResultRef == "" {
		u.Status = "failed"
		u.ErrorCode = "unsupported_usage"
		u.ErrorMessage = "Incomplete batch results cannot be settled"
		return u, nil
	}
	result, status, err := s.qianwenResourceRequest(ctx, a, http.MethodGet, "/files/"+url.PathEscape(u.ResultRef)+"/content", "application/json", nil)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	if status < 200 || status >= 300 {
		return resourceFailure(status), nil
	}
	u.Usage, err = validateQianwenBatchResult(j, result)
	if err != nil {
		u.Status = "failed"
		u.ErrorCode = "unsupported_usage"
		u.ErrorMessage = "Unrecognized batch result usage; no settlement"
		return u, nil
	}
	return u, nil
}
func validateQianwenBatchResult(j *UpstreamJob, raw []byte) (json.RawMessage, error) {
	if j.Billing.FileBatch == nil || j.Billing.Model != QianwenBatchTestModel || !utf8.Valid(raw) {
		return nil, ErrUpstreamJobUnsupportedUsage
	}
	expected := map[string]bool{}
	for _, id := range j.Billing.FileBatch.CustomIDHashes {
		expected[id] = true
	}
	var input, output int64
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		if !json.Valid(line) {
			return nil, ErrUpstreamJobUnsupportedUsage
		}
		row := gjson.ParseBytes(line)
		hash := fileBatchHash([]byte(row.Get("custom_id").String()))
		if !expected[hash] || row.Get("response.status_code").Int() != 200 || row.Get("response.body.model").String() != QianwenBatchTestModel || (row.Get("error").Exists() && row.Get("error").Type != gjson.Null) {
			return nil, ErrUpstreamJobUnsupportedUsage
		}
		delete(expected, hash)
		usage := row.Get("response.body.usage")
		unknown := false
		usage.ForEach(func(k, v gjson.Result) bool {
			if k.String() != "prompt_tokens" && k.String() != "completion_tokens" && k.String() != "total_tokens" {
				unknown = true
			}
			return true
		})
		if unknown {
			return nil, ErrUpstreamJobUnsupportedUsage
		}
		for _, name := range []string{"prompt_tokens", "completion_tokens", "total_tokens"} {
			v := usage.Get(name)
			if v.Type != gjson.Number || v.Float() != float64(v.Int()) || v.Int() < 0 || v.Int() > 1000000 {
				return nil, ErrUpstreamJobUnsupportedUsage
			}
		}
		if usage.Get("total_tokens").Int() != usage.Get("prompt_tokens").Int()+usage.Get("completion_tokens").Int() {
			return nil, ErrUpstreamJobUnsupportedUsage
		}
		input += usage.Get("prompt_tokens").Int()
		output += usage.Get("completion_tokens").Int()
	}
	if len(expected) != 0 {
		return nil, ErrUpstreamJobUnsupportedUsage
	}
	return json.Marshal(map[string]any{"free_test_model": true, "validated_items": len(j.Billing.FileBatch.CustomIDHashes), "prompt_tokens": input, "completion_tokens": output, "result_sha256": fileBatchHash(raw)})
}
func (s *OpenAIGatewayService) QianwenResourceContent(ctx context.Context, j *UpstreamJob) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if j.Status != "succeeded" || (j.Family != "files" && j.Family != "batch") {
		return nil, errors.New("resource content unavailable")
	}
	a, err := s.QianwenResourceAccount(ctx, j, false)
	if err != nil {
		return nil, err
	}
	raw, status, err := s.qianwenResourceRequest(ctx, a, http.MethodGet, "/files/"+url.PathEscape(j.ResultRef)+"/content", "application/json", nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, errors.New("resource content unavailable")
	}
	expected := gjson.GetBytes(j.UsageJSON, "result_sha256").String()
	if j.Family == "files" && j.Billing.FileBatch != nil {
		expected = j.Billing.FileBatch.SHA256
	}
	if expected == "" || fileBatchHash(raw) != expected {
		return nil, errors.New("resource content integrity mismatch")
	}
	return raw, nil
}
