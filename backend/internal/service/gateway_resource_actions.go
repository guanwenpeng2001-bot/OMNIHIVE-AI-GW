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
	"mime"
	"mime/multipart"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Standard resource actions (files, batches, message batches, moderations) share
// one admission layer: ownership, validated request metadata and a frozen tariff
// are persisted through the shared upstream job service before any upstream call.
// Provider wire shapes stay in the platform action adapters.
const (
	// GatewayResourceFileLimit bounds one uploaded file. A larger upload receives
	// an explicit rejection instead of being truncated or silently forwarded.
	GatewayResourceFileLimit = 64 << 20
	// GatewayResourceBatchLineLimit bounds one batch definition.
	GatewayResourceBatchLineLimit = 100000
	// GatewayResourceResultLimit bounds one result download used for settlement.
	GatewayResourceResultLimit = 128 << 20
	// GatewayResourceDownloadLimit bounds one client-facing content stream.
	GatewayResourceDownloadLimit = 512 << 20
	// GatewayResourceBatchDiscount is the documented batch token discount applied
	// to the caller's frozen standard model tariff.
	GatewayResourceBatchDiscount = 0.5

	GatewayResourcePricingModeTokens = "tokens"
	GatewayResourcePricingModeFree   = "free"

	GatewayResourceFilePurposeBatch = "batch"
	GatewayResourceOutputPrefix     = "output-"
)

var (
	ErrGatewayResourceFileTooLarge = errors.New("uploaded file exceeds the gateway resource file limit")
	ErrGatewayResourceFileInvalid  = errors.New("uploaded file is not a valid batch input")
	// ErrGatewayResourcePagination reports a cursor that does not name one of the
	// caller's resources, so a listing never answers a foreign or unknown cursor
	// with a silently empty page.
	ErrGatewayResourcePagination = errors.New("invalid resource pagination cursor")

	gatewayResourceIdentifierPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	gatewayResourcePurposePattern    = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	// gatewayResourceBatchEndpoints mirrors the batch targets the provider
	// adapters accept, so an unverifiable target never reaches admission.
	gatewayResourceBatchEndpoints = []string{
		"/v1/responses", "/v1/chat/completions", "/v1/embeddings", "/v1/completions",
		"/v1/moderations", "/v1/images/generations", "/v1/images/edits", "/v1/videos",
	}
	// gatewayResourceSettledBatchEndpoints are the batch targets this shared layer
	// can price from result token usage. Image and video batches keep their
	// dedicated pipelines (image batch actions and the native video APIs).
	gatewayResourceSettledBatchEndpoints = []string{
		"/v1/responses", "/v1/chat/completions", "/v1/embeddings", "/v1/completions", "/v1/moderations",
	}
)

// GatewayResourceRepository is the optional store extension the shared resource
// actions need. Unrelated job adapters and repository fakes stay intact.
type GatewayResourceRepository interface {
	ListGatewayResources(context.Context, string, int64, int64, int64, string, string, string, int) ([]*UpstreamJob, error)
	GatewayResourceFileInUse(context.Context, string) (bool, error)
}

// GatewayResourceFileSnapshot is admission-time metadata for one uploaded file.
// Only hashes, counts and the declared purpose are persisted; file contents and
// prompts never enter the database.
type GatewayResourceFileSnapshot struct {
	Purpose        string   `json:"purpose,omitempty"`
	Filename       string   `json:"filename,omitempty"`
	PayloadSHA256  string   `json:"payload_sha256,omitempty"`
	Bytes          int      `json:"bytes,omitempty"`
	Lines          int      `json:"lines,omitempty"`
	BatchModel     string   `json:"batch_model,omitempty"`
	BatchEndpoint  string   `json:"batch_endpoint,omitempty"`
	CustomIDHashes []string `json:"custom_id_hashes,omitempty"`
}

// GatewayResourceBatchSnapshot is the immutable admission-time tariff and
// identity of one batch job. TokenPrices already include the batch discount, so
// settlement never re-resolves today's catalogue.
type GatewayResourceBatchSnapshot struct {
	Endpoint                string        `json:"endpoint,omitempty"`
	InputFileID             string        `json:"input_file_id,omitempty"`
	Model                   string        `json:"model,omitempty"`
	UpstreamModel           string        `json:"upstream_model,omitempty"`
	PricingMode             string        `json:"pricing_mode,omitempty"`
	TokenPrices             *ModelPricing `json:"token_prices,omitempty"`
	PriceSource             string        `json:"price_source,omitempty"`
	PriceVersion            string        `json:"price_version,omitempty"`
	PricingAt               time.Time     `json:"pricing_at,omitempty"`
	UserGroupRateMultiplier float64       `json:"user_group_rate_multiplier,omitempty"`
	TimeMultiplier          float64       `json:"time_multiplier,omitempty"`
	BatchDiscount           float64       `json:"batch_discount,omitempty"`
	RequestCount            int           `json:"request_count,omitempty"`
	CustomIDHashes          []string      `json:"custom_id_hashes,omitempty"`
}

// GatewayResourceBatchAdmission carries the frozen admission decisions of one
// batch. StandardPrices is the caller's resolved standard (non-discounted)
// tariff; the batch discount is applied here, once, at admission.
type GatewayResourceBatchAdmission struct {
	Platform                string
	Model                   string
	UpstreamModel           string
	Endpoint                string
	PricingMode             string
	StandardPrices          *ModelPricing
	PriceSource             string
	PriceVersion            string
	PricingAt               time.Time
	UserGroupRateMultiplier float64
	RequestCount            int
	CustomIDHashes          []string
	InputFileID             string
}

func gatewayResourceOptionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func gatewayResourceHash(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func gatewayResourceIdentifier(id string) bool {
	return gatewayResourceIdentifierPattern.MatchString(id)
}

func gatewayResourceContains(values []string, candidate string) bool {
	return slices.Contains(values, candidate)
}

// GatewayResourceBatchTokenPricing derives batch token rates from a frozen
// standard tariff. Priority and flex tiers do not exist for batch work.
func GatewayResourceBatchTokenPricing(standard ModelPricing) ModelPricing {
	standard.InputPricePerToken *= GatewayResourceBatchDiscount
	standard.OutputPricePerToken *= GatewayResourceBatchDiscount
	standard.CacheCreationPricePerToken *= GatewayResourceBatchDiscount
	standard.CacheReadPricePerToken *= GatewayResourceBatchDiscount
	standard.CacheCreation5mPrice *= GatewayResourceBatchDiscount
	standard.CacheCreation1hPrice *= GatewayResourceBatchDiscount
	standard.InputPricePerTokenPriority = 0
	standard.OutputPricePerTokenPriority = 0
	standard.CacheCreationPricePerTokenPriority = 0
	standard.CacheReadPricePerTokenPriority = 0
	standard.FastMultiplier, standard.FlexMultiplier = nil, nil
	return standard
}

// ValidateGatewayResourceFile validates one uploaded resource file. Batch inputs
// are parsed line by line so a mixed or unverifiable batch never reaches the
// upstream: every admitted line must be a POST of one model to one endpoint.
func ValidateGatewayResourceFile(purpose, filename string, raw []byte) (*GatewayResourceFileSnapshot, error) {
	if !gatewayResourcePurposePattern.MatchString(purpose) {
		return nil, fmt.Errorf("%w: purpose is required", ErrGatewayResourceFileInvalid)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: file is empty", ErrGatewayResourceFileInvalid)
	}
	if len(raw) > GatewayResourceFileLimit {
		return nil, ErrGatewayResourceFileTooLarge
	}
	snapshot := &GatewayResourceFileSnapshot{Purpose: purpose, Filename: filename, PayloadSHA256: gatewayResourceHash(raw), Bytes: len(raw)}
	if purpose != GatewayResourceFilePurposeBatch {
		return snapshot, nil
	}
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("%w: batch input must be UTF-8", ErrGatewayResourceFileInvalid)
	}
	seen := make(map[string]bool)
	lines := bytes.Split(raw, []byte("\n"))
	if len(lines) > 0 && len(bytes.TrimSpace(lines[len(lines)-1])) == 0 {
		// One trailing newline terminates the final request.
		lines = lines[:len(lines)-1]
	}
	for _, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			return nil, fmt.Errorf("%w: blank batch line", ErrGatewayResourceFileInvalid)
		}
		if !gjson.ValidBytes(line) || !gjson.ParseBytes(line).IsObject() {
			return nil, fmt.Errorf("%w: every line must be a JSON object", ErrGatewayResourceFileInvalid)
		}
		row := gjson.ParseBytes(line)
		customID := row.Get("custom_id")
		if customID.Type != gjson.String || !gatewayResourceIdentifier(customID.String()) || seen[customID.String()] {
			return nil, fmt.Errorf("%w: custom_id must be unique and match %s", ErrGatewayResourceFileInvalid, gatewayResourceIdentifierPattern)
		}
		seen[customID.String()] = true
		if len(seen) > GatewayResourceBatchLineLimit {
			return nil, fmt.Errorf("%w: at most %d requests per batch", ErrGatewayResourceFileInvalid, GatewayResourceBatchLineLimit)
		}
		endpoint := row.Get("url").String()
		model := row.Get("body.model").String()
		if row.Get("method").String() != "POST" || !gatewayResourceContains(gatewayResourceBatchEndpoints, endpoint) ||
			!row.Get("body").IsObject() || strings.TrimSpace(model) == "" || row.Get("body.stream").Bool() {
			return nil, fmt.Errorf("%w: every request must be a non-streaming POST with a model and a documented endpoint", ErrGatewayResourceFileInvalid)
		}
		if snapshot.BatchModel != "" && (snapshot.BatchModel != model || snapshot.BatchEndpoint != endpoint) {
			return nil, fmt.Errorf("%w: all requests must share one model and endpoint", ErrGatewayResourceFileInvalid)
		}
		snapshot.BatchModel, snapshot.BatchEndpoint = model, endpoint
		snapshot.CustomIDHashes = append(snapshot.CustomIDHashes, gatewayResourceHash([]byte(customID.String())))
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("%w: batch input requires at least one request", ErrGatewayResourceFileInvalid)
	}
	snapshot.Lines = len(seen)
	return snapshot, nil
}

// GatewayResourceBatchEndpointSettleable reports whether the shared layer can
// settle this batch target from result usage.
func GatewayResourceBatchEndpointSettleable(endpoint string) bool {
	return gatewayResourceContains(gatewayResourceSettledBatchEndpoints, endpoint)
}

// GatewayResourceBatchBillingSnapshot freezes the tariff and ownership of one
// batch. Free targets (moderations) keep an explicit free pricing state instead
// of inventing a token tariff.
func GatewayResourceBatchBillingSnapshot(admission GatewayResourceBatchAdmission, group *Group, account *Account, key *APIKey, subscription *UserSubscription) (UpstreamJobBilling, error) {
	snapshot := &GatewayResourceBatchSnapshot{
		Endpoint:       admission.Endpoint,
		InputFileID:    admission.InputFileID,
		Model:          admission.Model,
		UpstreamModel:  admission.UpstreamModel,
		PricingMode:    admission.PricingMode,
		PriceSource:    admission.PriceSource,
		PriceVersion:   admission.PriceVersion,
		PricingAt:      admission.PricingAt,
		BatchDiscount:  GatewayResourceBatchDiscount,
		RequestCount:   admission.RequestCount,
		CustomIDHashes: append([]string(nil), admission.CustomIDHashes...),
	}
	billing := UpstreamJobBilling{
		PricingState:          PricingStateUnpriced,
		Model:                 admission.Model,
		RequestedModel:        admission.Model,
		RateMultiplier:        1,
		AccountRateMultiplier: 1,
		ResourceBatch:         snapshot,
	}
	if admission.PricingMode == GatewayResourcePricingModeFree {
		billing.PricingState = PricingStateFree
		if group != nil {
			billing.RateMultiplier = group.RateMultiplier
		}
		if account != nil {
			billing.AccountRateMultiplier = account.BillingRateMultiplier()
		}
		return billing, nil
	}
	if admission.PricingMode != GatewayResourcePricingModeTokens || admission.StandardPrices == nil || group == nil || account == nil || key == nil {
		return billing, ErrUpstreamJobTariff
	}
	discounted := GatewayResourceBatchTokenPricing(*admission.StandardPrices)
	if admission.Platform == PlatformAnthropic {
		discounted = AnthropicMessageBatchTokenPricing(*admission.StandardPrices)
	}
	multiplier := admission.UserGroupRateMultiplier
	if !finitePositive(multiplier) {
		multiplier = 1
	}
	timeMultiplier := group.PeakMultiplierAt(admission.PricingAt)
	if timeMultiplier <= 0 {
		timeMultiplier = 1
	}
	billing.RateMultiplier = multiplier * timeMultiplier
	billing.AccountRateMultiplier = account.BillingRateMultiplier()
	if !finitePositive(billing.RateMultiplier) || !finitePositive(billing.AccountRateMultiplier) {
		return billing, ErrUpstreamJobTariff
	}
	billing.PricingState = PricingStatePriced
	snapshot.TokenPrices = &discounted
	snapshot.UserGroupRateMultiplier = multiplier
	snapshot.TimeMultiplier = timeMultiplier
	billing.AccountQuota = account.HasAnyQuotaLimit()
	billing.KeyQuota, billing.KeyRateLimit = key.Quota > 0, key.HasRateLimits()
	if subscription != nil {
		billing.SubscriptionID = &subscription.ID
	}
	return billing, nil
}

// GatewayResourceFileBillingSnapshot records one file upload. File storage is not
// a billable action, so the tariff stays explicitly free and no token price is
// invented.
func GatewayResourceFileBillingSnapshot(snapshot *GatewayResourceFileSnapshot, account *Account, key *APIKey, subscription *UserSubscription) UpstreamJobBilling {
	billing := UpstreamJobBilling{PricingState: PricingStateFree, RateMultiplier: 1, AccountRateMultiplier: 1, ResourceFile: snapshot}
	if account != nil {
		billing.AccountRateMultiplier = account.BillingRateMultiplier()
		billing.AccountQuota = account.HasAnyQuotaLimit()
	}
	if key != nil {
		billing.KeyQuota, billing.KeyRateLimit = key.Quota > 0, key.HasRateLimits()
	}
	if subscription != nil {
		billing.SubscriptionID = &subscription.ID
	}
	return billing
}

// GatewayResourceBatchUsage is the settlement evidence of one finished batch. It
// is written by the poll path and consumed by the charge path, so both sides
// share one validated shape.
type GatewayResourceBatchUsage struct {
	RequestCounts   map[string]int64 `json:"request_counts"`
	ProviderStatus  string           `json:"provider_status,omitempty"`
	Lines           int              `json:"lines"`
	Matched         int              `json:"matched"`
	ServerToolUses  int              `json:"server_tool_uses"`
	InputTokens     int              `json:"input_tokens"`
	OutputTokens    int              `json:"output_tokens"`
	CachedTokens    int              `json:"cached_input_tokens"`
	CacheCreation   int              `json:"cache_creation_tokens"`
	CacheCreation5m int              `json:"cache_creation_5m_tokens"`
	CacheCreation1h int              `json:"cache_creation_1h_tokens"`
	CacheRead       int              `json:"cache_read_tokens"`
}

type gatewayResourceBatchUsagePayload struct {
	RequestCounts  map[string]int64 `json:"request_counts"`
	ProviderStatus string           `json:"provider_status,omitempty"`
	Result         struct {
		Lines           int `json:"lines"`
		Matched         int `json:"matched"`
		ServerToolUses  int `json:"server_tool_uses"`
		InputTokens     int `json:"input_tokens"`
		OutputTokens    int `json:"output_tokens"`
		CachedTokens    int `json:"cached_input_tokens"`
		CacheCreation   int `json:"cache_creation_tokens"`
		CacheCreation5m int `json:"cache_creation_5m_tokens"`
		CacheCreation1h int `json:"cache_creation_1h_tokens"`
		CacheRead       int `json:"cache_read_tokens"`
	} `json:"result"`
}

// GatewayResourceBatchUsageJSON renders the settlement evidence of one batch.
func GatewayResourceBatchUsageJSON(usage GatewayResourceBatchUsage) (json.RawMessage, error) {
	if usage.Lines < 0 || usage.Matched < 0 || usage.Matched > usage.Lines || usage.Lines > GatewayResourceBatchLineLimit {
		return nil, ErrUpstreamJobUnsupportedUsage
	}
	payload := gatewayResourceBatchUsagePayload{RequestCounts: usage.RequestCounts, ProviderStatus: usage.ProviderStatus}
	payload.Result.Lines, payload.Result.Matched, payload.Result.ServerToolUses = usage.Lines, usage.Matched, usage.ServerToolUses
	payload.Result.InputTokens, payload.Result.OutputTokens, payload.Result.CachedTokens = usage.InputTokens, usage.OutputTokens, usage.CachedTokens
	payload.Result.CacheCreation, payload.Result.CacheCreation5m, payload.Result.CacheCreation1h = usage.CacheCreation, usage.CacheCreation5m, usage.CacheCreation1h
	payload.Result.CacheRead = usage.CacheRead
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(encoded), nil
}

// gatewayResourceBatchCountsConsistent proves the stored provider counters
// account for the admitted batch exactly: every admitted request reached a
// terminal state and the billable successes match the validated result lines.
func gatewayResourceBatchCountsConsistent(platform string, counts map[string]int64, requestCount, matched, lines int) bool {
	if counts == nil || requestCount <= 0 || matched < 0 || lines < matched {
		return false
	}
	switch platform {
	case PlatformOpenAI:
		total, totalOK := counts["total"]
		completed, completedOK := counts["completed"]
		failed, failedOK := counts["failed"]
		if !totalOK || !completedOK || !failedOK || len(counts) != 3 {
			return false
		}
		return total == int64(requestCount) && completed+failed == total && matched == int(completed) && lines == int(total)
	case PlatformAnthropic:
		if len(counts) != 5 || counts["processing"] != 0 {
			return false
		}
		terminal := counts["succeeded"] + counts["errored"] + counts["canceled"] + counts["expired"]
		return terminal == int64(requestCount) && matched == int(counts["succeeded"]) && lines == int(terminal)
	default:
		return false
	}
}

// GatewayResourceBatchRequestCounts exposes the stored provider counts for
// resource serialization.
func GatewayResourceBatchRequestCounts(job *UpstreamJob) map[string]int64 {
	if job == nil || len(job.UsageJSON) == 0 {
		return nil
	}
	counts := gjson.GetBytes(job.UsageJSON, "request_counts")
	if !counts.IsObject() {
		return nil
	}
	parsed := make(map[string]int64)
	valid := true
	counts.ForEach(func(key, value gjson.Result) bool {
		if value.Type != gjson.Number || value.Float() != float64(value.Int()) || value.Int() < 0 || value.Int() > GatewayResourceBatchLineLimit {
			valid = false
			return false
		}
		parsed[key.String()] = value.Int()
		return true
	})
	if !valid {
		return nil
	}
	return parsed
}

func gatewayResourceTokenCount(value gjson.Result) (int, bool) {
	if value.Type != gjson.Number || value.Float() != float64(value.Int()) || value.Int() < 0 || value.Int() > 1<<40 {
		return 0, false
	}
	return int(value.Int()), true
}

// gatewayResourceSettledTokens validates the stored settlement evidence and
// returns the token quantities to price. Missing optional categories read as
// zero; an unrecognized or inconsistent result is never billed.
func gatewayResourceSettledTokens(job *UpstreamJob) (UsageTokens, bool) {
	if job == nil || len(job.UsageJSON) == 0 {
		return UsageTokens{}, false
	}
	usage := gjson.ParseBytes(job.UsageJSON)
	result := usage.Get("result")
	if !result.IsObject() {
		return UsageTokens{}, false
	}
	lines, linesOK := gatewayResourceTokenCount(result.Get("lines"))
	matched, matchedOK := gatewayResourceTokenCount(result.Get("matched"))
	serverTools, serverToolsOK := gatewayResourceTokenCount(result.Get("server_tool_uses"))
	if !linesOK || !matchedOK || !serverToolsOK || matched > lines || serverTools > 0 {
		return UsageTokens{}, false
	}
	read := func(name string) (int, bool) {
		value := result.Get(name)
		if !value.Exists() {
			return 0, true
		}
		return gatewayResourceTokenCount(value)
	}
	input, inputOK := read("input_tokens")
	output, outputOK := read("output_tokens")
	cached, cachedOK := read("cached_input_tokens")
	cacheCreation, cacheCreationOK := read("cache_creation_tokens")
	cacheCreation5m, cacheCreation5mOK := read("cache_creation_5m_tokens")
	cacheCreation1h, cacheCreation1hOK := read("cache_creation_1h_tokens")
	cacheRead, cacheReadOK := read("cache_read_tokens")
	if !inputOK || !outputOK || !cachedOK || !cacheCreationOK || !cacheCreation5mOK || !cacheCreation1hOK || !cacheReadOK {
		return UsageTokens{}, false
	}
	if cached > input {
		return UsageTokens{}, false
	}
	if cacheCreation5m+cacheCreation1h > cacheCreation && cacheCreation5m+cacheCreation1h > 0 {
		return UsageTokens{}, false
	}
	return UsageTokens{
		InputTokens:           input,
		OutputTokens:          output,
		CacheReadTokens:       cacheRead + cached,
		CacheCreationTokens:   cacheCreation,
		CacheCreation5mTokens: cacheCreation5m,
		CacheCreation1hTokens: cacheCreation1h,
	}, true
}

func gatewayResourceSettledLines(job *UpstreamJob) (lines, matched int) {
	if job == nil || len(job.UsageJSON) == 0 {
		return 0, 0
	}
	result := gjson.GetBytes(job.UsageJSON, "result")
	lines, _ = gatewayResourceTokenCount(result.Get("lines"))
	matched, _ = gatewayResourceTokenCount(result.Get("matched"))
	return lines, matched
}

// GatewayResourceJobCharge prices one shared resource job from its frozen
// admission snapshot. It never resolves today's catalogue and never invents a
// tariff: an unusable snapshot fails the job closed for review.
func GatewayResourceJobCharge(job *UpstreamJob) (*UsageLog, *UsageBillingCommand, error) {
	if job == nil {
		return nil, nil, ErrUpstreamJobUnsupportedUsage
	}
	switch {
	case job.Platform == PlatformOpenAI && job.Family == "realtime_turn":
		return OpenAIRealtimeCharge(job)
	case job.Platform == PlatformOpenAI && job.Family == "files":
		return gatewayResourceFreeCharge(job, "openai_files", "file-upload-v1")
	case job.Platform == PlatformOpenAI && job.Family == "batch":
		return gatewayResourceBatchCharge(job, "openai_batch", "openai-batch-v1")
	case job.Platform == PlatformAnthropic && job.Family == "batch":
		return gatewayResourceBatchCharge(job, "anthropic_message_batch", "anthropic-message-batch-v1")
	case job.Platform == PlatformOpenRouter && job.Family == "video":
		return OpenRouterVideoJobCharge(job)
	case job.Platform == PlatformOpenAI && job.Family == "video":
		return OpenAIVideoJobCharge(job)
	default:
		return QianwenAsyncJobCharge(job)
	}
}

func gatewayResourceFreeCharge(job *UpstreamJob, source, version string) (usageLog *UsageLog, command *UsageBillingCommand, chargeErr error) {
	defer func() { recordJobPricingFailure(job, chargeErr) }()
	if job.Billing.ResourceFile == nil {
		return nil, nil, ErrUpstreamJobUnsupportedUsage
	}
	billing := job.Billing
	log := &UsageLog{
		UserID: job.UserID, APIKeyID: job.APIKeyID, AccountID: job.AccountID, GroupID: job.GroupID,
		RequestID: job.PublicRequestID, RateMultiplier: 1, AccountRateMultiplier: &billing.AccountRateMultiplier,
		SubscriptionID: billing.SubscriptionID, BillingMode: gatewayResourceOptionalString("file"), PriceSource: gatewayResourceOptionalString(source),
		PriceCardVersion: gatewayResourceOptionalString(version), RequestType: RequestTypeSync, CreatedAt: time.Now().UTC(),
	}
	zeroCost := 0.0
	log.AccountStatsCost = &zeroCost
	command = gatewayResourceBillingCommand(job, billing, 0, 0)
	if err := freezeJobPricing(job, log, PricingSnapshot{TokenPrices: &ModelPricing{}, Mode: "file", Source: source}); err != nil {
		return nil, nil, err
	}
	return log, command, nil
}

func gatewayResourceBatchCharge(job *UpstreamJob, source, version string) (usageLog *UsageLog, command *UsageBillingCommand, chargeErr error) {
	defer func() { recordJobPricingFailure(job, chargeErr) }()
	billing, snapshot := job.Billing, job.Billing.ResourceBatch
	if snapshot == nil || snapshot.Model == "" || snapshot.PricingMode == "" || snapshot.RequestCount <= 0 {
		return nil, nil, ErrUpstreamJobUnsupportedUsage
	}
	if billing.Model != snapshot.Model || billing.RequestedModel != snapshot.Model {
		return nil, nil, ErrUpstreamJobUnsupportedUsage
	}
	if len(job.UsageJSON) == 0 {
		return nil, nil, ErrUpstreamJobUnsupportedUsage
	}
	counts := GatewayResourceBatchRequestCounts(job)
	tokens, ok := gatewayResourceSettledTokens(job)
	if !ok {
		return nil, nil, ErrUpstreamJobUnsupportedUsage
	}
	settledLines, settledMatched := gatewayResourceSettledLines(job)
	if !gatewayResourceBatchCountsConsistent(job.Platform, counts, snapshot.RequestCount, settledMatched, settledLines) {
		return nil, nil, ErrUpstreamJobUnsupportedUsage
	}
	switch snapshot.PricingMode {
	case GatewayResourcePricingModeFree:
		return gatewayResourceFreeChargeForBatch(job, source, version)
	case GatewayResourcePricingModeTokens:
		if snapshot.TokenPrices == nil || len(snapshot.CustomIDHashes) != snapshot.RequestCount {
			return nil, nil, ErrUpstreamJobUnsupportedUsage
		}
	default:
		return nil, nil, ErrUpstreamJobUnsupportedUsage
	}
	if !finitePositive(billing.RateMultiplier) || !finitePositive(billing.AccountRateMultiplier) {
		return nil, nil, ErrUpstreamJobTariff
	}
	if err := validateTokenDimensions(snapshot.TokenPrices, tokens); err != nil {
		return nil, nil, err
	}
	cost := (&BillingService{}).computeTokenBreakdown(snapshot.TokenPrices, tokens, billing.RateMultiplier, "", false)
	if cost == nil {
		return nil, nil, ErrUpstreamJobUnsupportedUsage
	}
	total := QuantizeUsageBillingAmount(cost.TotalCost)
	actual := QuantizeUsageBillingAmount(cost.ActualCost)
	accountCost := QuantizeUsageBillingAmount(total * billing.AccountRateMultiplier)
	for _, amount := range []float64{cost.InputCost, cost.OutputCost, cost.CacheCreationCost, cost.CacheReadCost, total, actual, accountCost} {
		if ValidateUsageBillingAmount(amount) != nil {
			return nil, nil, ErrUpstreamJobUnsupportedUsage
		}
	}
	log := &UsageLog{
		UserID: job.UserID, APIKeyID: job.APIKeyID, AccountID: job.AccountID, GroupID: job.GroupID,
		RequestID: job.PublicRequestID, Model: billing.Model, RequestedModel: billing.RequestedModel,
		InputTokens: tokens.InputTokens, OutputTokens: tokens.OutputTokens,
		CacheCreationTokens: tokens.CacheCreationTokens, CacheReadTokens: tokens.CacheReadTokens,
		CacheCreation5mTokens: tokens.CacheCreation5mTokens, CacheCreation1hTokens: tokens.CacheCreation1hTokens,
		InputCost: cost.InputCost, OutputCost: cost.OutputCost, CacheCreationCost: cost.CacheCreationCost, CacheReadCost: cost.CacheReadCost,
		TotalCost: total, ActualCost: actual, RateMultiplier: billing.RateMultiplier,
		AccountRateMultiplier: &billing.AccountRateMultiplier, AccountStatsCost: &accountCost, SubscriptionID: billing.SubscriptionID,
		BillingMode: gatewayResourceOptionalString(source), PriceSource: gatewayResourceOptionalString(snapshot.PriceSource), PriceCardVersion: gatewayResourceOptionalString(version),
		RequestType: RequestTypeSync, CreatedAt: time.Now().UTC(),
	}
	if snapshot.UpstreamModel != "" {
		log.UpstreamModel = &snapshot.UpstreamModel
	}
	frozenPrices := *snapshot.TokenPrices
	if err := freezeJobPricing(job, log, PricingSnapshot{Tokens: tokens, TokenPrices: &frozenPrices, Mode: source, Source: snapshot.PriceSource}); err != nil {
		return nil, nil, err
	}
	return log, gatewayResourceBillingCommand(job, billing, actual, accountCost), nil
}

func gatewayResourceFreeChargeForBatch(job *UpstreamJob, source, version string) (*UsageLog, *UsageBillingCommand, error) {
	billing := job.Billing
	log := &UsageLog{
		UserID: job.UserID, APIKeyID: job.APIKeyID, AccountID: job.AccountID, GroupID: job.GroupID,
		RequestID: job.PublicRequestID, Model: billing.Model, RequestedModel: billing.RequestedModel,
		RateMultiplier: 1, AccountRateMultiplier: &billing.AccountRateMultiplier, SubscriptionID: billing.SubscriptionID,
		BillingMode: gatewayResourceOptionalString(source), PriceSource: gatewayResourceOptionalString(source), PriceCardVersion: gatewayResourceOptionalString(version),
		RequestType: RequestTypeSync, CreatedAt: time.Now().UTC(),
	}
	zeroCost := 0.0
	log.AccountStatsCost = &zeroCost
	if err := freezeJobPricing(job, log, PricingSnapshot{TokenPrices: &ModelPricing{}, Mode: source, Source: source}); err != nil {
		return nil, nil, err
	}
	return log, gatewayResourceBillingCommand(job, billing, 0, 0), nil
}

func gatewayResourceBillingCommand(job *UpstreamJob, billing UpstreamJobBilling, actual, accountCost float64) *UsageBillingCommand {
	command := &UsageBillingCommand{
		RequestID: job.PublicRequestID, RequestFingerprint: job.RequestFingerprint,
		UserID: job.UserID, APIKeyID: job.APIKeyID, AccountID: job.AccountID, AccountType: AccountTypeAPIKey,
		Model: billing.Model, BalanceCost: actual,
	}
	if billing.SubscriptionID != nil {
		command.BillingType = BillingTypeSubscription
		command.SubscriptionID = billing.SubscriptionID
		command.SubscriptionCost = actual
		command.BalanceCost = 0
	}
	if billing.KeyQuota {
		command.APIKeyQuotaCost = actual
	}
	if billing.KeyRateLimit {
		command.APIKeyRateLimitCost = actual
	}
	if billing.AccountQuota {
		command.AccountQuotaCost = accountCost
	}
	return command
}

// GetGatewayResource resolves one public resource id for a user and key. Files
// carry no model, so only ownership applies; batch reads additionally honour the
// pinned model access policy through the shared job accessor.
func (s *UpstreamJobService) GetGatewayResource(ctx context.Context, id string, userID, keyID int64, platform, family string) (*UpstreamJob, error) {
	if s == nil || s.repo == nil || id == "" {
		return nil, ErrUpstreamJobNotFound
	}
	if family == "files" {
		job, err := s.repo.Get(ctx, id, userID, keyID)
		if err != nil {
			return nil, err
		}
		if job == nil || job.Platform != platform || job.Family != family {
			return nil, ErrUpstreamJobNotFound
		}
		return job, nil
	}
	job, err := s.Get(ctx, id, userID, keyID)
	if err != nil {
		return nil, err
	}
	if job.Platform != platform || job.Family != family {
		return nil, ErrUpstreamJobNotFound
	}
	return job, nil
}

// ListGatewayResources returns one page of the caller's files or batches for one
// platform. Batch rows whose model is no longer visible are withheld.
func (s *UpstreamJobService) ListGatewayResources(ctx context.Context, platform string, userID, keyID, group int64, family, purpose, after string, limit int) ([]*UpstreamJob, error) {
	repository, ok := s.repo.(GatewayResourceRepository)
	if !ok {
		return nil, errors.New("resource store unavailable")
	}
	if limit < 1 || limit > 100 {
		return nil, ErrUpstreamResourceConflict
	}
	switch family {
	case "files":
		if purpose != "" && purpose != GatewayResourceFilePurposeBatch && purpose != "batch_output" {
			return nil, ErrUpstreamResourceConflict
		}
	default:
		if purpose != "" {
			return nil, ErrUpstreamResourceConflict
		}
	}
	if after != "" {
		// A batch output is addressed by its virtual id, but the cursor is stored
		// under the owning batch. Resolve it under the caller's ownership so a
		// foreign or unknown cursor is reported instead of returning an empty page.
		cursorFamily, cursorID := "files", after
		if family != "files" {
			cursorFamily, cursorID = "batch", strings.TrimPrefix(after, GatewayResourceOutputPrefix)
		}
		cursor, err := s.GetGatewayResource(ctx, cursorID, userID, keyID, platform, cursorFamily)
		if err != nil || cursor.GroupID == nil || *cursor.GroupID != group {
			return nil, ErrGatewayResourcePagination
		}
		after = cursorID
	}
	jobs, err := repository.ListGatewayResources(ctx, platform, userID, keyID, group, family, purpose, after, limit+1)
	if err != nil {
		return nil, err
	}
	visible := jobs[:0]
	for _, job := range jobs {
		if job.Family == "batch" && (!visibleModelPolicyAllows(ctx, job.Billing.RequestedModel) || !modelAccessAccountAllows(ctx, job.AccountID, job.Billing.RequestedModel)) {
			continue
		}
		visible = append(visible, job)
	}
	return visible, nil
}

// WithGatewayResourceInput serializes a child batch against deletion of its input
// file using the same durable parent lock the shared job service uses.
func (s *UpstreamJobService) WithGatewayResourceInput(ctx context.Context, id string, platform string, userID, keyID, group int64, submit func() (*UpstreamJob, error)) (*UpstreamJob, error) {
	if _, err := s.GetGatewayResource(ctx, id, userID, keyID, platform, "files"); err != nil {
		return nil, err
	}
	var result *UpstreamJob
	_, err := s.repo.WithLocked(ctx, id, func(job *UpstreamJob, _ UpstreamJobTx) error {
		if job == nil || job.Platform != platform || job.Family != "files" || job.UserID != userID || job.APIKeyID != keyID || job.GroupID == nil || *job.GroupID != group || job.ResourceState.Deleted {
			return ErrUpstreamJobNotFound
		}
		if job.Status != "succeeded" {
			return ErrUpstreamResourceConflict
		}
		var submitErr error
		result, submitErr = submit()
		return submitErr
	})
	return result, err
}

// ManageGatewayResource applies one explicit resource transition (cancel, delete,
// delete_output) to an owner-bound resource. Cross-tenant ids stay
// indistinguishable from missing ids.
func (s *UpstreamJobService) ManageGatewayResource(ctx context.Context, id string, platform, family string, userID, keyID, group int64, action string, remote func(context.Context, *UpstreamJob, string) error) (*UpstreamJob, error) {
	if _, err := s.GetGatewayResource(ctx, id, userID, keyID, platform, family); err != nil {
		return nil, err
	}
	return s.repo.WithLocked(ctx, id, func(job *UpstreamJob, _ UpstreamJobTx) error {
		if job == nil || job.Platform != platform || job.UserID != userID || job.APIKeyID != keyID || job.GroupID == nil || *job.GroupID != group {
			return ErrUpstreamJobNotFound
		}
		switch action {
		case "cancel":
			if job.Family != "batch" || job.ResourceState.Deleted {
				return ErrUpstreamJobNotFound
			}
			if job.Terminal() || job.ResourceState.CancelRequested {
				return nil
			}
			if job.UpstreamTaskID == "" {
				return ErrUpstreamResourceConflict
			}
		case "delete":
			if job.Family != "files" {
				return ErrUpstreamJobNotFound
			}
			if job.ResourceState.Deleted {
				return nil
			}
			if !job.Terminal() {
				return ErrUpstreamResourceConflict
			}
			repository, ok := s.repo.(GatewayResourceRepository)
			if !ok {
				return errors.New("resource store unavailable")
			}
			used, err := repository.GatewayResourceFileInUse(ctx, job.PublicRequestID)
			if err != nil {
				return err
			}
			if used {
				return ErrUpstreamResourceConflict
			}
		case "delete_output":
			if job.Family != "batch" {
				return ErrUpstreamJobNotFound
			}
			if job.ResourceState.OutputDeleted {
				return nil
			}
			if !job.Terminal() || job.ChargedUsageLogID == nil || job.ResultRef == "" {
				return ErrUpstreamResourceConflict
			}
		default:
			return ErrUpstreamResourceConflict
		}
		networkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := remote(networkCtx, job, action); err != nil {
			return err
		}
		switch action {
		case "cancel":
			job.ResourceState.CancelRequested = true
			job.ResourceState.ProviderStatus = "cancelling"
			job.NextPollAt = s.now()
		case "delete":
			job.ResourceState.Deleted = true
		case "delete_output":
			job.ResourceState.OutputDeleted = true
		}
		return nil
	})
}

// GatewayResourceBatchRequest is the validated metadata of one batch create.
type GatewayResourceBatchRequest struct {
	InputFileID      string
	Endpoint         string
	CompletionWindow string
}

// GatewayResourceBatchUpstreamBody rewrites one admitted batch create body so the
// provider receives the file id it issued instead of the gateway's public id.
// Only that field changes: every other admitted byte is preserved, and the
// client body stays the idempotency fingerprint.
func GatewayResourceBatchUpstreamBody(body []byte, upstreamFileID string) ([]byte, error) {
	if strings.TrimSpace(upstreamFileID) == "" {
		return nil, ErrUpstreamResourceConflict
	}
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return nil, errors.New("batch body must be a JSON object")
	}
	if gjson.GetBytes(body, "input_file_id").String() == upstreamFileID {
		return append([]byte(nil), body...), nil
	}
	rewritten, err := sjson.SetBytes(append([]byte(nil), body...), "input_file_id", upstreamFileID)
	if err != nil {
		return nil, err
	}
	return rewritten, nil
}

// ParseGatewayResourceBatchRequest validates one batch create body. Unknown keys
// are refused rather than forwarded, so an unverified target never reaches the
// upstream billing path.
func ParseGatewayResourceBatchRequest(body []byte) (GatewayResourceBatchRequest, error) {
	request := GatewayResourceBatchRequest{}
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return request, errors.New("batch body must be a JSON object")
	}
	parsed := gjson.ParseBytes(body)
	unknown := false
	parsed.ForEach(func(key, _ gjson.Result) bool {
		switch key.String() {
		case "input_file_id", "endpoint", "completion_window", "metadata":
		default:
			unknown = true
		}
		return true
	})
	request.InputFileID = parsed.Get("input_file_id").String()
	request.Endpoint = parsed.Get("endpoint").String()
	request.CompletionWindow = parsed.Get("completion_window").String()
	if unknown || !gatewayResourceIdentifier(request.InputFileID) {
		return request, errors.New("unsupported batch fields or input_file_id")
	}
	if !gatewayResourceContains(gatewayResourceBatchEndpoints, request.Endpoint) {
		return request, errors.New("unverified batch endpoint")
	}
	if !GatewayResourceBatchEndpointSettleable(request.Endpoint) {
		return request, errors.New("this endpoint keeps its dedicated batch pipeline")
	}
	if request.CompletionWindow != "24h" {
		return request, errors.New("unverified batch completion window")
	}
	if metadata := parsed.Get("metadata"); metadata.Exists() && metadata.Type != gjson.Null && !metadata.IsObject() {
		return request, errors.New("batch metadata must be an object")
	}
	return request, nil
}

// ParseGatewayResourceMultipart extracts the single uploaded file, its purpose
// and its filename while preserving the original multipart bytes for faithful
// forwarding.
func ParseGatewayResourceMultipart(contentType string, raw []byte, limit int) (string, string, []byte, error) {
	mediaType, parameters, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" || parameters["boundary"] == "" {
		return "", "", nil, errors.New("file upload requires multipart/form-data")
	}
	reader := multipart.NewReader(bytes.NewReader(raw), parameters["boundary"])
	purpose, filename := "", ""
	var content []byte
	fileSeen, purposeSeen := false, false
	for {
		part, partErr := reader.NextPart()
		if partErr == io.EOF {
			break
		}
		if partErr != nil {
			return "", "", nil, errors.New("invalid multipart body")
		}
		switch part.FormName() {
		case "file":
			if fileSeen || part.FileName() == "" {
				_ = part.Close()
				return "", "", nil, errors.New("exactly one file part is required")
			}
			data, readErr := io.ReadAll(io.LimitReader(part, int64(limit)+1))
			_ = part.Close()
			if readErr != nil {
				return "", "", nil, readErr
			}
			if len(data) > limit {
				return "", "", nil, ErrGatewayResourceFileTooLarge
			}
			fileSeen, filename, content = true, part.FileName(), data
		case "purpose":
			if purposeSeen {
				_ = part.Close()
				return "", "", nil, errors.New("duplicate purpose field")
			}
			data, _ := io.ReadAll(io.LimitReader(part, 256))
			_ = part.Close()
			purposeSeen, purpose = true, strings.TrimSpace(string(data))
		default:
			// Unknown fields (for example expires_after) are forwarded verbatim by
			// the adapter and are not part of resource admission.
			_, _ = io.Copy(io.Discard, io.LimitReader(part, 4096))
			_ = part.Close()
		}
	}
	if !fileSeen || !purposeSeen {
		return "", "", nil, errors.New("expected one file part and one purpose field")
	}
	return purpose, filename, content, nil
}
