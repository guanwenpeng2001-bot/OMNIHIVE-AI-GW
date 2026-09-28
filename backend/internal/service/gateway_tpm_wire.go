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
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/gjson"
)

type tpmFailureBody struct {
	io.ReadCloser
	plan     *GatewayRequestPlan
	identity TPMReservationIdentity
	buffer   bytes.Buffer
	tooLarge bool
	once     sync.Once
}

func (body *tpmFailureBody) Read(destination []byte) (int, error) {
	count, err := body.ReadCloser.Read(destination)
	if body.buffer.Len()+count <= 2<<20 {
		_, _ = body.buffer.Write(destination[:count])
	} else {
		body.tooLarge = true
	}
	return count, err
}
func (body *tpmFailureBody) Close() error {
	body.once.Do(func() {
		if body.tooLarge {
			return
		}
		usage, known := normalizedTPMFailureUsage(body.buffer.Bytes())
		if !known {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Failed attempt facts affect only rate counters, never dollar billing.
		_ = body.plan.SettleTPMAttempt(ctx, body.identity.AttemptIndex, usage)
	})
	return body.ReadCloser.Close()
}
func normalizedTPMFailureUsage(payload []byte) (UsageTokens, bool) {
	if !json.Valid(payload) {
		return UsageTokens{}, false
	}
	usage := gjson.GetBytes(payload, "usage")
	if !usage.Exists() {
		usage = gjson.GetBytes(payload, "response.usage")
	}
	input, output := usage.Get("input_tokens"), usage.Get("output_tokens")
	cached, created := usage.Get("cache_read_input_tokens"), usage.Get("cache_creation_input_tokens")
	inclusive := false
	if usage.Get("prompt_tokens").Exists() {
		input = usage.Get("prompt_tokens")
		output = usage.Get("completion_tokens")
		cached = usage.Get("prompt_tokens_details.cached_tokens")
		inclusive = true
	}
	if detail := usage.Get("input_tokens_details.cached_tokens"); detail.Exists() {
		cached = detail
		inclusive = true
	}
	parse := func(value gjson.Result, required bool) (int64, bool) {
		if !value.Exists() {
			return 0, !required
		}
		if value.Type != gjson.Number {
			return 0, false
		}
		quantity, err := strconv.ParseInt(value.Raw, 10, 64)
		return quantity, err == nil && quantity >= 0
	}
	inputCount, inputOK := parse(input, true)
	outputCount, outputOK := parse(output, true)
	cacheCount, cacheOK := parse(cached, false)
	createCount, createOK := parse(created, false)
	if !inputOK || !outputOK || !cacheOK || !createOK {
		return UsageTokens{}, false
	}
	if inclusive {
		if cacheCount > inputCount {
			return UsageTokens{}, false
		}
		inputCount -= cacheCount
	}
	quantities := UsageTokens{InputTokens: int(inputCount), OutputTokens: int(outputCount), CacheReadTokens: int(cacheCount), CacheCreationTokens: int(createCount)}
	_, err := TPMUsageTokens(quantities)
	return quantities, err == nil
}

type tpmWireKey struct{}
type tpmWireRequest struct {
	body []byte
	path string
}

func withTPMWire(ctx context.Context, body []byte, path string) context.Context {
	return context.WithValue(ctx, tpmWireKey{}, tpmWireRequest{body: body, path: path})
}

// Use the reviewed model's ENTIRE accepted input capacity, not a heuristic
// tokenizer. It includes provider templates, system/tools and conversion extras.
// This intentionally over-reserves small prompts. Unknown model limits, remote
// history and unbounded modalities are refused under an enabled policy.
func (p *GatewayRequestPlan) estimateTPMWire(ctx context.Context, accountID int64) TPMEstimate {
	wire, _ := ctx.Value(tpmWireKey{}).(tpmWireRequest)
	if len(wire.body) == 0 || !json.Valid(wire.body) || p.catalogRepo == nil {
		return TPMEstimate{}
	}
	account, err := p.catalogRepo.GetByID(ctx, accountID)
	if err != nil || account == nil {
		return TPMEstimate{}
	}
	model := gjson.GetBytes(wire.body, "model").String()
	if model == "" && strings.Contains(wire.path, "/models/") {
		model = strings.Split(strings.Split(wire.path, "/models/")[1], ":")[0]
	}
	if model == "" && strings.Contains(wire.path, "/model/") {
		model = strings.Split(strings.SplitN(wire.path, "/model/", 2)[1], "/invoke")[0]
	}
	if model == "" {
		return TPMEstimate{}
	}
	record, known := p.registrySnapshot.CapabilitySnapshot().LookupExact(resolveCapabilityProfile(account), model)
	if !known {
		return TPMEstimate{}
	}
	payload := wire.body
	if request := gjson.GetBytes(payload, "request"); request.IsObject() {
		payload = []byte(request.Raw)
	}
	path := wire.path
	if strings.Contains(path, "/model/") && gjson.GetBytes(payload, "anthropic_version").Exists() {
		path = "/messages"
	}
	return estimateTPMFromRecord(payload, path, record)
}

// Continuous realtime protocols can generate outside explicit bounded turns.
// Read authoritative policies before opening that upstream session, with an
// unknown estimate so any enabled TPM scope rejects it without a token hold.
func checkUnboundedTPMSession(ctx context.Context, accountID int64) error {
	plan := GatewayRequestPlanFromContext(ctx)
	if plan == nil {
		return nil
	}
	plan.mu.Lock()
	plan.tpmSequence++
	sequence := plan.tpmSequence
	plan.mu.Unlock()
	_, err := plan.ReserveTPMAttempt(ctx, sequence, accountID, fmt.Sprintf("unbounded-session:%d", accountID), TPMEstimate{})
	return err
}

func estimateTPMFromRecord(body []byte, path string, record CapabilityRecord) TPMEstimate {
	if record.Source == "" || gjson.GetBytes(body, "previous_response_id").Exists() || gjson.GetBytes(body, "conversation").Exists() {
		return TPMEstimate{}
	}
	// Image/audio/video/realtime inputs cannot be bounded by a text context cap.
	text := strings.ToLower(string(body))
	for _, marker := range []string{"image_url", "input_image", "inline_data", "inlinedata", "file_data", "filedata", "input_audio", "audio_url", "input_file", "cachedcontent", "cached_content", "google_search", "googlesearch", "code_execution", "codeexecution"} {
		if strings.Contains(text, `"`+marker+`"`) {
			return TPMEstimate{}
		}
	}
	unsafeContent := false
	var inspect func(gjson.Result)
	inspect = func(value gjson.Result) {
		if !value.IsObject() && !value.IsArray() {
			return
		}
		kind := value.Get("type").String()
		switch kind {
		case "image", "document", "audio", "video", "input_image", "input_audio", "input_file":
			unsafeContent = true
		}
		value.ForEach(func(_ gjson.Result, child gjson.Result) bool { inspect(child); return !unsafeContent })
	}
	inspect(gjson.ParseBytes(body))
	if unsafeContent {
		return TPMEstimate{}
	}
	for _, tool := range gjson.GetBytes(body, "tools").Array() {
		if kind := tool.Get("type").String(); kind != "" && kind != "function" {
			return TPMEstimate{}
		}
	}
	input := int64(0)
	if record.MaxInputTokens != nil {
		input = *record.MaxInputTokens
	}
	if input <= 0 && (record.ContextLimitKind == "input" || record.ContextLimitKind == "total") {
		input = record.ContextWindow
	}
	if input <= 0 {
		return TPMEstimate{}
	}
	if strings.Contains(path, "embedding") || strings.Contains(path, "embedContent") || strings.Contains(path, "EmbedContents") {
		batch := int64(1)
		value := gjson.GetBytes(body, "input")
		if value.IsArray() {
			values := value.Array()
			if len(values) > 0 && values[0].Type != gjson.Number {
				batch = int64(len(values))
			}
		}
		if requests := gjson.GetBytes(body, "requests"); requests.IsArray() {
			batch = int64(len(requests.Array()))
		}
		if batch < 1 || input > math.MaxInt64/batch {
			return TPMEstimate{}
		}
		return TPMEstimate{InputBound: input * batch, Candidates: 1, Verified: true}
	}
	if !(strings.Contains(path, "messages") || strings.Contains(path, "completions") || strings.Contains(path, "responses") || strings.Contains(path, "generateContent") || strings.Contains(path, "GenerateContent")) {
		return TPMEstimate{}
	}
	if strings.Contains(strings.ToLower(path), "batch") {
		return TPMEstimate{}
	}
	output := int64(0)
	for _, name := range []string{"max_completion_tokens", "max_output_tokens", "max_tokens", "generationConfig.maxOutputTokens", "generation_config.max_output_tokens"} {
		value := gjson.GetBytes(body, name)
		if value.Exists() {
			if value.Type != gjson.Number || value.Int() <= 0 || fmt.Sprint(value.Int()) != value.Raw {
				return TPMEstimate{}
			}
			// Legacy max_tokens does not bound hidden reasoning on OpenAI models.
			if name == "max_tokens" && strings.Contains(path, "completions") && (record.SupportsReasoning == nil || *record.SupportsReasoning) {
				return TPMEstimate{}
			}
			output = value.Int()
			break
		}
	}
	if output <= 0 {
		return TPMEstimate{}
	}
	if strings.Contains(strings.ToLower(path), "generatecontent") && (record.SupportsReasoning == nil || *record.SupportsReasoning) {
		thinking := gjson.GetBytes(body, "generationConfig.thinkingConfig.thinkingBudget")
		if !thinking.Exists() {
			thinking = gjson.GetBytes(body, "generation_config.thinking_config.thinking_budget")
		}
		if thinking.Type != gjson.Number || thinking.Int() < 0 || fmt.Sprint(thinking.Int()) != thinking.Raw || thinking.Int() > math.MaxInt64-output {
			return TPMEstimate{}
		}
		output += thinking.Int()
	}
	candidates := int64(1)
	for _, name := range []string{"n", "best_of", "generationConfig.candidateCount", "generation_config.candidate_count"} {
		value := gjson.GetBytes(body, name)
		if value.Exists() {
			if value.Type != gjson.Number || value.Int() <= 0 || fmt.Sprint(value.Int()) != value.Raw {
				return TPMEstimate{}
			}
			if value.Int() > candidates {
				candidates = value.Int()
			}
		}
	}
	if !strings.Contains(path, "chat/completions") && strings.HasSuffix(path, "completions") {
		prompt := gjson.GetBytes(body, "prompt")
		if prompt.IsArray() {
			values := prompt.Array()
			if len(values) > 0 && values[0].Type != gjson.Number {
				batch := int64(len(values))
				if input > math.MaxInt64/batch || candidates > math.MaxInt64/batch {
					return TPMEstimate{}
				}
				input *= batch
				candidates *= batch
			}
		}
	}
	return TPMEstimate{InputBound: input, OutputBound: output, Candidates: candidates, Verified: true}
}

func IsTPMAdmissionError(err error) bool {
	var exceeded *TPMLimitExceeded
	return errors.As(err, &exceeded) || errors.Is(err, ErrTPMEstimateUnavailable) || errors.Is(err, ErrTPMStateUnavailable) || errors.Is(err, ErrTPMReservationConflict)
}

func (p *GatewayRequestPlan) beginTPMSend(ctx context.Context, accountID int64) (int, error) {
	if p == nil {
		return 0, nil
	}
	p.mu.Lock()
	p.tpmRejection = nil
	p.tpmSequence++
	sequence := p.tpmSequence
	p.mu.Unlock()
	wire, _ := ctx.Value(tpmWireKey{}).(tpmWireRequest)
	fingerprint := sha256.Sum256(append([]byte(fmt.Sprintf("%d:%s:", accountID, wire.path)), wire.body...))
	_, err := p.ReserveTPMAttempt(ctx, sequence, accountID, hex.EncodeToString(fingerprint[:]), p.estimateTPMWire(ctx, accountID))
	if err != nil {
		p.mu.Lock()
		p.tpmRejection = err
		p.mu.Unlock()
		var exceeded *TPMLimitExceeded
		if errors.As(err, &exceeded) && exceeded.Scope == TPMScopeAccount {
			return 0, &UpstreamFailoverError{StatusCode: 429, TPM: exceeded, ResponseBody: []byte(`{"error":{"code":"ACCOUNT_TPM_EXCEEDED","message":"token admission minute exhausted"}}`)}
		}
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	p.MarkTPMAttemptSent(sequence)
	return sequence, nil
}

func (p *GatewayRequestPlan) TPMAdmissionRejection() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.tpmRejection
}

func (p *GatewayRequestPlan) finishTPMSend(sequence int, err error) {
	if p == nil || sequence == 0 {
		return
	}
	var networkError *net.OpError
	if !errors.As(err, &networkError) || networkError.Op != "dial" {
		return
	}
	p.mu.Lock()
	attempt := p.tpmAttempts[sequence]
	if attempt != nil {
		attempt.sent = false
	}
	p.mu.Unlock()
	if attempt == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if p.tpmRepository().ReleaseUnsentTPMAttempt(ctx, attempt.reservation.TPMReservationIdentity) == nil {
		p.mu.Lock()
		attempt.settled = true
		p.mu.Unlock()
	}
}

func (p *GatewayRequestPlan) TPMReceiptIdentity(accountID int64) *TPMReservationIdentity {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for sequence := p.tpmSequence; sequence > 0; sequence-- {
		if attempt := p.tpmAttempts[sequence]; attempt != nil && attempt.accountID == accountID && attempt.sent {
			identity := attempt.reservation.TPMReservationIdentity
			return &identity
		}
	}
	return nil
}

func tpmHTTPRequestContext(request *http.Request) context.Context {
	if request.GetBody == nil {
		return request.Context()
	}
	body, err := request.GetBody()
	if err != nil {
		return request.Context()
	}
	defer body.Close()
	payload, err := io.ReadAll(io.LimitReader(body, 16<<20))
	if err != nil {
		return request.Context()
	}
	return withTPMWire(request.Context(), payload, request.URL.Path)
}
