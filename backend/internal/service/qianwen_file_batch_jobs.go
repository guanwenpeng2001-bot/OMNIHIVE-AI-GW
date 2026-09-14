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
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tidwall/gjson"
)

const QianwenFileLimit = 1 << 20
const QianwenBatchTestModel = "batch-test-model"

// File batch tariffs are distinct from synchronous Batch Chat prices.
type QianwenFileBatchSnapshot struct {
	UserGroupRateMultiplier float64                `json:"user_group_rate_multiplier,omitempty"`
	TimeMultiplier          float64                `json:"time_multiplier,omitempty"`
	PricingAt               time.Time              `json:"pricing_at,omitempty"`
	Model                   string                 `json:"model,omitempty"`
	Endpoint                string                 `json:"endpoint,omitempty"`
	InputPrice              float64                `json:"input_price,omitempty"`
	OutputPrice             float64                `json:"output_price,omitempty"`
	PriceVersion            string                 `json:"price_version,omitempty"`
	FX                      *QianwenPricingOptions `json:"fx,omitempty"`
	SHA256                  string                 `json:"sha256,omitempty"`
	Bytes                   int                    `json:"bytes,omitempty"`
	CustomIDHashes          []string               `json:"custom_id_hashes"`
	InputFileID             string                 `json:"input_file_id,omitempty"`
}

func fileBatchHash(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

// Normalize before hashing AND forwarding; multipart boundaries are irrelevant
// to idempotency. Persist only hashes so prompts/custom IDs stay out of the DB.
func ValidateQianwenBatchFile(raw []byte) ([]byte, *QianwenFileBatchSnapshot, error) {
	if len(raw) == 0 || len(raw) > QianwenFileLimit || !utf8.Valid(raw) {
		return nil, nil, errors.New("batch files must be UTF-8 JSONL up to 1 MiB")
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
		model, endpoint := row.Get("body.model").String(), row.Get("url").String()
		_, _, paid := qianwenFileBatchTariff(model)
		if row.Get("method").String() != "POST" || row.Get("body.stream").Bool() || !row.Get("body.messages").IsArray() || !((model == QianwenBatchTestModel && endpoint == "/v1/chat/ds-test") || (paid && endpoint == "/v1/chat/completions")) {
			return nil, nil, errors.New("only non-streaming diagnostic or explicitly priced text file batches are supported")
		}
		if snapshot.Model != "" && (snapshot.Model != model || snapshot.Endpoint != endpoint) {
			return nil, nil, errors.New("all batch rows must use one model and endpoint")
		}
		if paid {
			body := row.Get("body")
			unknown := false
			body.ForEach(func(k, v gjson.Result) bool {
				switch k.String() {
				case "model", "messages", "max_tokens", "temperature", "top_p", "seed", "stop", "stream", "enable_thinking":
				default:
					unknown = true
				}
				return true
			})
			thinking := body.Get("enable_thinking")
			if unknown || (thinking.Exists() && thinking.Type != gjson.False) {
				return nil, nil, errors.New("paid batches require non-thinking text requests with supported parameters")
			}
			messages := body.Get("messages").Array()
			if len(messages) == 0 {
				return nil, nil, errors.New("messages must not be empty")
			}
			for _, message := range messages {
				if message.Get("content").Type != gjson.String {
					return nil, nil, errors.New("paid file batches currently require plain text messages")
				}
				// Qwen-Long interprets fileid:// in system messages as a provider
				// file reference. Raw account-level IDs must not bypass tenant ownership.
				if strings.Contains(strings.ToLower(message.Get("content").String()), "fileid://") {
					return nil, nil, errors.New("provider file references are not supported in batch messages")
				}
			}
		}
		snapshot.Model, snapshot.Endpoint = model, endpoint
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
	if j.Platform != PlatformQianwen || j.Billing.FileBatch == nil || j.Billing.Model != j.Billing.FileBatch.BatchModel() || len(j.Billing.FileBatch.CustomIDHashes) == 0 {
		return nil, nil, ErrUpstreamJobUnsupportedUsage
	}
	var input, output int
	if j.Family == "files" {
		if j.UpstreamTaskID == "" || j.ResultRef != j.UpstreamTaskID || j.Billing.FileBatch.SHA256 == "" {
			return nil, nil, ErrUpstreamJobUnsupportedUsage
		}
	} else {
		usage := gjson.ParseBytes(j.UsageJSON)
		for _, name := range []string{"validated_items", "prompt_tokens", "completion_tokens"} {
			v := usage.Get(name)
			if v.Type != gjson.Number || v.Float() != float64(v.Int()) || v.Int() < 0 || v.Int() > 100000000 {
				return nil, nil, ErrUpstreamJobUnsupportedUsage
			}
		}
		if usage.Get("validated_items").Int() < 0 || usage.Get("validated_items").Int() > int64(len(j.Billing.FileBatch.CustomIDHashes)) || !usage.Get("validated_batch").Bool() || (usage.Get("validated_items").Int() > 0 && j.ResultRef == "") {
			return nil, nil, ErrUpstreamJobUnsupportedUsage
		}
		input = int(usage.Get("prompt_tokens").Int())
		output = int(usage.Get("completion_tokens").Int())
		if usage.Get("validated_items").Int() == 0 && input+output != 0 {
			return nil, nil, ErrUpstreamJobUnsupportedUsage
		}
	}
	return chargeQianwenFileBatch(j, input, output)
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
		return nil, resp.StatusCode, errors.New(qianwenResourceFailureMessage("resource upstream temporarily unavailable", raw))
	}
	return raw, resp.StatusCode, nil
}

// Suppress the entire reason when it could contain credentials, including short
// values under sensitive field names. Check before truncating to avoid leaking
// a credential whose identifying suffix would otherwise be cut off.
var qianwenResourceCredentialPattern = regexp.MustCompile(`(?i)sk-|bearer\s|api[_-]?key|authorization|password|token|[a-z0-9_+/=-]{32,}`)

func qianwenResourceFailureMessage(fallback string, raw []byte) string {
	if !json.Valid(raw) {
		return fallback
	}
	reason := gjson.GetBytes(raw, "error.message")
	if reason.Type != gjson.String {
		reason = gjson.GetBytes(raw, "errors.data.0.message")
	}
	if reason.Type != gjson.String || strings.TrimSpace(reason.String()) == "" {
		return fallback
	}
	message := strings.TrimSpace(reason.String())
	if qianwenResourceCredentialPattern.MatchString(message) {
		message = "[redacted]"
	}
	characters := []rune(fallback + ": " + message)
	const maxCharacters = 512
	if len(characters) > maxCharacters {
		return string(characters[:maxCharacters-1]) + "…"
	}
	return string(characters)
}

func resourceFailure(status int, raw []byte) UpstreamJobUpdate {
	return UpstreamJobUpdate{Status: "failed", ErrorCode: "upstream_rejected", ErrorMessage: qianwenResourceFailureMessage(fmt.Sprintf("DashScope rejected resource request (HTTP %d)", status), raw)}
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
		return resourceFailure(status, raw), nil
	}
	id := gjson.GetBytes(raw, "id").String()
	if !json.Valid(raw) || id == "" || gjson.GetBytes(raw, "purpose").String() != "batch" || gjson.GetBytes(raw, "status").String() != "processed" {
		return UpstreamJobUpdate{}, errors.New("invalid file upload response")
	}
	return UpstreamJobUpdate{TaskID: id, Status: "succeeded", ResultRef: id}, nil
}
func (s *OpenAIGatewayService) SubmitQianwenBatch(ctx context.Context, a *Account, file *UpstreamJob) (UpstreamJobUpdate, error) {
	rawBody, _ := json.Marshal(map[string]string{"input_file_id": file.UpstreamTaskID, "endpoint": file.Billing.FileBatch.BatchEndpoint(), "completion_window": "24h"})
	raw, status, err := s.qianwenResourceRequest(ctx, a, http.MethodPost, "/batches", "application/json", rawBody)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	if status < 200 || status >= 300 {
		return resourceFailure(status, raw), nil
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
	u := UpstreamJobUpdate{TaskID: gjson.GetBytes(raw, "id").String(), ResultRef: gjson.GetBytes(raw, "output_file_id").String(), ProviderStatus: gjson.GetBytes(raw, "status").String()}
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
	case "failed":
		u.Status = "failed"
		u.ErrorCode = "upstream_failed"
		u.ErrorMessage = qianwenResourceFailureMessage("DashScope batch failed or was cancelled", raw)
	case "expired", "cancelled":
		// Successful rows still incur charges; terminal evidence is validated below.
		u.Status = "succeeded"
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
		return resourceFailure(status, raw), nil
	}
	u, err := parseQianwenBatch(raw)
	if err != nil || u.Status != "succeeded" || u.TaskID != j.UpstreamTaskID {
		return u, err
	}
	counts := gjson.GetBytes(raw, "request_counts")
	validCounts := j.Billing.FileBatch != nil
	for _, name := range []string{"failed", "completed", "total"} {
		v := counts.Get(name)
		validCounts = validCounts && v.Type == gjson.Number && v.Float() == float64(v.Int()) && v.Int() >= 0 && v.Int() <= 100
	}
	if !validCounts || counts.Get("total").Int() != int64(len(j.Billing.FileBatch.CustomIDHashes)) || counts.Get("completed").Int()+counts.Get("failed").Int() > counts.Get("total").Int() || (u.ProviderStatus == "completed" && counts.Get("completed").Int()+counts.Get("failed").Int() != counts.Get("total").Int()) || (counts.Get("completed").Int() > 0 && u.ResultRef == "") {
		u.Status = "failed"
		u.ErrorCode = "unsupported_usage"
		u.ErrorMessage = "Incomplete batch results cannot be settled"
		return u, nil
	}
	var result []byte
	if u.ResultRef != "" {
		result, status, err = s.qianwenResourceRequest(ctx, a, http.MethodGet, "/files/"+url.PathEscape(u.ResultRef)+"/content", "application/json", nil)
	}
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	if status < 200 || status >= 300 {
		return resourceFailure(status, result), nil
	}
	u.Usage, err = validateQianwenBatchItems(j, result, int(counts.Get("completed").Int()))
	if err != nil {
		u.Status = "failed"
		u.ErrorCode = "unsupported_usage"
		u.ErrorMessage = "Unrecognized batch result usage; no settlement"
		return u, nil
	}
	return u, nil
}
func validateQianwenBatchResult(j *UpstreamJob, raw []byte) (json.RawMessage, error) {
	if j.Billing.FileBatch == nil {
		return nil, ErrUpstreamJobUnsupportedUsage
	}
	return validateQianwenBatchItems(j, raw, len(j.Billing.FileBatch.CustomIDHashes))
}

func validateQianwenBatchItems(j *UpstreamJob, raw []byte, completed int) (json.RawMessage, error) {
	if j.Billing.FileBatch == nil || j.Billing.Model != j.Billing.FileBatch.BatchModel() || !utf8.Valid(raw) || completed < 0 || completed > len(j.Billing.FileBatch.CustomIDHashes) {
		return nil, ErrUpstreamJobUnsupportedUsage
	}
	expected := map[string]bool{}
	for _, id := range j.Billing.FileBatch.CustomIDHashes {
		expected[id] = true
	}
	var input, output int64
	validated := 0
	lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
	if len(bytes.TrimSpace(raw)) == 0 && completed == 0 {
		lines = nil
	}
	for _, line := range lines {
		if !json.Valid(line) {
			return nil, ErrUpstreamJobUnsupportedUsage
		}
		row := gjson.ParseBytes(line)
		hash := fileBatchHash([]byte(row.Get("custom_id").String()))
		if !expected[hash] || row.Get("response.status_code").Int() != 200 || row.Get("response.body.model").String() != j.Billing.Model || (row.Get("error").Exists() && row.Get("error").Type != gjson.Null) {
			return nil, ErrUpstreamJobUnsupportedUsage
		}
		delete(expected, hash)
		validated++
		usage := row.Get("response.body.usage")
		unknown := false
		usage.ForEach(func(k, v gjson.Result) bool {
			if k.String() == "prompt_tokens_details" || k.String() == "completion_tokens_details" {
				if !v.IsObject() {
					unknown = true
					return true
				}
				v.ForEach(func(dk, dv gjson.Result) bool {
					if (dk.String() != "cached_tokens" && dk.String() != "reasoning_tokens") || dv.Type != gjson.Number || dv.Float() != 0 {
						unknown = true
					}
					return true
				})
				return true
			}
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
	if validated != completed {
		return nil, ErrUpstreamJobUnsupportedUsage
	}
	return json.Marshal(map[string]any{"free_test_model": j.Billing.Model == QianwenBatchTestModel, "validated_batch": true, "validated_items": validated, "prompt_tokens": input, "completion_tokens": output, "result_sha256": fileBatchHash(raw)})
}
func (s *OpenAIGatewayService) QianwenResourceContent(ctx context.Context, j *UpstreamJob) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if j.Status != "succeeded" || j.ResultRef == "" || j.ResourceState.Deleted || (j.Family == "batch" && j.ResourceState.OutputDeleted) || (j.Family != "files" && j.Family != "batch") {
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
		return nil, errors.New(qianwenResourceFailureMessage("resource content unavailable", raw))
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
