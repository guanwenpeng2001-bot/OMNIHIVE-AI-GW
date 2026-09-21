package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/tidwall/gjson"
)

// Provider wire traffic for the standard resource actions. Only fixed resource
// paths built from opaque ids reach these transports; provider content URLs and
// client supplied task ids are never followed, and every call re-resolves the
// account pinned at admission.
var errGatewayResourceStreamTooLarge = errors.New("gateway resource stream exceeds the configured bound")

// GatewayResourcePollJob builds the shared job poll function for the standard
// resource families. Providers that already own a poller keep it; only the
// resource families added here are dispatched differently.
func GatewayResourcePollJob(openAI *OpenAIGatewayService) func(context.Context, *UpstreamJob) (UpstreamJobUpdate, error) {
	return func(ctx context.Context, job *UpstreamJob) (UpstreamJobUpdate, error) {
		if job == nil {
			return UpstreamJobUpdate{}, ErrUpstreamJobUnsupportedUsage
		}
		switch {
		case job.Platform == PlatformOpenAI && job.Family == "files":
			// A stored file is already final; its job settles at submission.
			return UpstreamJobUpdate{TaskID: job.UpstreamTaskID, Status: "succeeded"}, nil
		case job.Platform == PlatformOpenAI && job.Family == "batch":
			return openAI.pollGatewayResourceOpenAIBatch(ctx, job)
		case job.Platform == PlatformAnthropic && job.Family == "batch":
			return openAI.pollGatewayResourceAnthropicBatch(ctx, job)
		case job.Platform == PlatformOpenAI && job.Family == "video":
			return openAI.PollOpenAIVideo(ctx, job)
		default:
			return openAI.PollQianwenAsyncJob(ctx, job)
		}
	}
}

// ResolveGatewayResourceStandardPricing resolves the caller's effective standard
// tariff for one model. Group and channel overrides win over the catalogue, and a
// missing price is reported instead of defaulting to free.
func (s *OpenAIGatewayService) ResolveGatewayResourceStandardPricing(ctx context.Context, group *Group, model string) (*ModelPricing, string, error) {
	if strings.TrimSpace(model) == "" {
		return nil, "", ErrModelPricingUnavailable
	}
	if s.resolver != nil && group != nil {
		resolved := s.resolver.Resolve(ctx, PricingInput{Model: model, GroupID: &group.ID, Group: group})
		if resolved == nil || resolved.BasePricing == nil {
			return nil, "", fmt.Errorf("%w for model: %s", ErrModelPricingUnavailable, model)
		}
		return resolved.BasePricing, resolved.Source, nil
	}
	if s.billingService == nil {
		return nil, "", ErrModelPricingUnavailable
	}
	pricing, err := s.billingService.GetModelPricing(model)
	if err != nil {
		return nil, "", err
	}
	return pricing, "catalog", nil
}

// ResolveOpenAIResourceAccount revalidates the account pinned at admission. A
// disabled or re-typed account must not keep serving a stored resource, and a
// submission additionally requires the account to stay schedulable in the group.
func (s *OpenAIGatewayService) ResolveOpenAIResourceAccount(ctx context.Context, job *UpstreamJob, submit bool) (*Account, error) {
	account, err := s.openAIResourceAccount(ctx, job)
	if err != nil {
		return nil, err
	}
	if submit && (job.GroupID == nil || !slices.Contains(account.GroupIDs, *job.GroupID)) {
		return nil, errors.New("OpenAI resource account is outside the active group")
	}
	return account, nil
}

// AuthorizeGatewayResourceNativeModel admits one batch model for one account.
// Batch requests are forwarded verbatim, so the admitted model must be the exact
// native model the account serves: a mapped or unavailable model is refused
// explicitly instead of being rewritten behind the caller's back.
func (s *OpenAIGatewayService) AuthorizeGatewayResourceNativeModel(ctx context.Context, account *Account, model, endpoint string) (string, error) {
	if account == nil || strings.TrimSpace(model) == "" {
		return "", ErrAccountModelUnsupported
	}
	if !visibleModelPolicyAllows(ctx, model) || !modelAccessAccountAllows(ctx, account.ID, model) {
		return "", ErrAccountModelUnsupported
	}
	if account.GetMappedModel(model) != model {
		return "", fmt.Errorf("%w: batch requests require the exact native model", ErrAccountModelUnsupported)
	}
	if !AccountSupportsResolvedModel(ctx, account, model, AccountModelBindingOptions{FinalModel: model, Endpoint: endpoint}) {
		return "", ErrAccountModelUnsupported
	}
	return model, nil
}

// GatewayResourceBindingEndpoint maps one batch target to the endpoint evidence
// used for model authorization.
func GatewayResourceBindingEndpoint(endpoint string) string {
	switch endpoint {
	case "/v1/chat/completions", "/v1/completions":
		return CompositeRouteEndpointChatCompletions
	case "/v1/responses":
		return CompositeRouteEndpointResponses
	case "/v1/embeddings":
		return CompositeRouteEndpointEmbeddings
	default:
		return CompositeRouteEndpointAny
	}
}

// AnthropicMessageBatchAdmission is the validated admission metadata of one
// native message batch.
type AnthropicMessageBatchAdmission struct {
	Model          string
	RequestCount   int
	CustomIDHashes []string
}

// ValidateAnthropicMessageBatchAdmission reuses the native batch validation and
// then extracts the single served model plus the admitted request identities.
func ValidateAnthropicMessageBatchAdmission(body []byte) (AnthropicMessageBatchAdmission, error) {
	if err := ValidateAnthropicMessageBatch(body); err != nil {
		return AnthropicMessageBatchAdmission{}, err
	}
	var batch struct {
		Requests []struct {
			CustomID string `json:"custom_id"`
			Params   struct {
				Model string `json:"model"`
			} `json:"params"`
		} `json:"requests"`
	}
	if err := json.Unmarshal(body, &batch); err != nil {
		return AnthropicMessageBatchAdmission{}, err
	}
	admission := AnthropicMessageBatchAdmission{RequestCount: len(batch.Requests)}
	for _, request := range batch.Requests {
		model := strings.TrimSpace(request.Params.Model)
		if admission.Model != "" && admission.Model != model {
			return AnthropicMessageBatchAdmission{}, fmt.Errorf("%w: all batch requests must use one model", ErrGatewayResourceFileInvalid)
		}
		admission.Model = model
		admission.CustomIDHashes = append(admission.CustomIDHashes, gatewayResourceHash([]byte(request.CustomID)))
	}
	if admission.Model == "" {
		return AnthropicMessageBatchAdmission{}, fmt.Errorf("%w: batch requests require a model", ErrGatewayResourceFileInvalid)
	}
	return admission, nil
}

func gatewayResourceProxyURL(account *Account) string {
	if account != nil && account.ProxyID != nil && account.Proxy != nil {
		return account.Proxy.URL()
	}
	return ""
}

// openAIResourceStream opens one streaming read of a native resource.
func (s *OpenAIGatewayService) openAIResourceStream(ctx context.Context, account *Account, endpoint string) (*http.Response, error) {
	credential, err := openAIActionCredential(account)
	if err != nil {
		return nil, err
	}
	target, err := s.openAIActionURL(account, endpoint)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	account.ApplyHeaderOverrides(request.Header)
	request.Header.Set("Authorization", "Bearer "+credential)
	request = request.WithContext(WithHTTPUpstreamProfile(request.Context(), HTTPUpstreamProfileOpenAI))
	return s.doOpenAIUpstream(request, gatewayResourceProxyURL(account), account)
}

// OpenOpenAIResourceContent streams one owner-bound file or batch output. The
// caller has already resolved ownership through the shared job service.
func (s *OpenAIGatewayService) OpenOpenAIResourceContent(ctx context.Context, job *UpstreamJob) (*http.Response, error) {
	if job == nil || job.Platform != PlatformOpenAI {
		return nil, ErrUpstreamJobNotFound
	}
	resourceID := ""
	switch job.Family {
	case "files":
		if job.ResourceState.Deleted {
			return nil, ErrUpstreamJobNotFound
		}
		resourceID = job.UpstreamTaskID
	case "batch":
		if job.Status != "succeeded" || job.ResourceState.OutputDeleted {
			return nil, ErrUpstreamResourceConflict
		}
		resourceID = job.ResultRef
	default:
		return nil, ErrUpstreamJobNotFound
	}
	if !openAIResourceID(resourceID) {
		return nil, ErrUpstreamResourceConflict
	}
	account, err := s.openAIResourceAccount(ctx, job)
	if err != nil {
		return nil, err
	}
	return s.openAIResourceStream(ctx, account, "/files/"+url.PathEscape(resourceID)+"/content")
}

// ManageOpenAIResource applies one explicit OpenAI resource transition. The
// caller owns the row lock and the state change.
func (s *OpenAIGatewayService) ManageOpenAIResource(ctx context.Context, job *UpstreamJob, action string) error {
	account, err := s.openAIResourceAccount(ctx, job)
	if err != nil {
		return err
	}
	method, endpoint, expected := http.MethodDelete, "/files/"+url.PathEscape(job.UpstreamTaskID), job.UpstreamTaskID
	switch action {
	case "cancel":
		if job.Family != "batch" {
			return ErrUpstreamJobNotFound
		}
		method, endpoint = http.MethodPost, "/batches/"+url.PathEscape(job.UpstreamTaskID)+"/cancel"
	case "delete_output":
		if job.Family != "batch" {
			return ErrUpstreamJobNotFound
		}
		endpoint, expected = "/files/"+url.PathEscape(job.ResultRef), job.ResultRef
	case "delete":
		if job.Family != "files" {
			return ErrUpstreamJobNotFound
		}
	default:
		return ErrUpstreamResourceConflict
	}
	if expected == "" {
		// A file upload that never reached the provider has nothing to remove.
		return nil
	}
	raw, status, err := s.openAIResourceRequest(ctx, account, method, endpoint, "", nil)
	if err != nil {
		return err
	}
	if action != "cancel" && status == http.StatusNotFound {
		// Retry after a local rollback of an already applied remote deletion.
		return nil
	}
	if status < 200 || status >= 300 {
		return errors.New("resource operation rejected upstream")
	}
	if action == "cancel" {
		switch gjson.GetBytes(raw, "status").String() {
		case "cancelling", "cancelled", "completed":
			return nil
		}
		return errors.New("invalid cancellation response")
	}
	if gjson.GetBytes(raw, "id").String() != expected || !gjson.GetBytes(raw, "deleted").Bool() {
		return errors.New("invalid deletion response")
	}
	return nil
}

// openAIBatchRequestCounts reads the provider request counters of one batch
// object. Unrecognized counters are reported as absent instead of guessed.
func openAIBatchRequestCounts(raw []byte) map[string]int64 {
	counts := gjson.GetBytes(raw, "request_counts")
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

func openAIBatchCountsFullyAccounted(counts map[string]int64) bool {
	total, totalOK := counts["total"]
	completed, completedOK := counts["completed"]
	failed, failedOK := counts["failed"]
	return totalOK && completedOK && failedOK && completed+failed == total
}

// pollGatewayResourceOpenAIBatch reads one batch and, once the provider reports
// completion, validates its output file before the shared job service settles it.
// A cancelled batch still settles its completed portion: the provider bills the
// requests that ran, and the resource keeps reporting the cancellation.
func (s *OpenAIGatewayService) pollGatewayResourceOpenAIBatch(ctx context.Context, job *UpstreamJob) (UpstreamJobUpdate, error) {
	account, err := s.openAIResourceAccount(ctx, job)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	raw, status, err := s.openAIResourceRequest(ctx, account, http.MethodGet, "/batches/"+url.PathEscape(job.UpstreamTaskID), "", nil)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	if status < 200 || status >= 300 {
		return openAIResourceFailure(status, raw), nil
	}
	update, err := parseOpenAIBatchResource(raw)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	usage := GatewayResourceBatchUsage{RequestCounts: openAIBatchRequestCounts(raw), ProviderStatus: update.ProviderStatus}
	if update.Status == "failed" && update.ErrorCode == "upstream_cancelled" {
		// Keep polling until the provider accounting is final, then settle the
		// completed portion instead of discarding executed work.
		if !openAIBatchCountsFullyAccounted(usage.RequestCounts) {
			update.Status, update.ErrorCode, update.ErrorMessage = "running", "", ""
			return s.finishGatewayResourceUsage(job, update, usage)
		}
		update.Status, update.ErrorCode = "succeeded", ""
	}
	if update.Status == "succeeded" {
		settled, settleErr := s.settleOpenAIBatchResult(ctx, account, job, update, usage)
		if settleErr != nil {
			if errors.Is(settleErr, ErrUpstreamJobUnsupportedUsage) || errors.Is(settleErr, errGatewayResourceStreamTooLarge) {
				update.Status = "failed"
				update.ErrorCode = "unsupported_usage"
				update.ErrorMessage = "Batch results cannot be priced; evidence retained for review"
				return s.finishGatewayResourceUsage(job, update, usage)
			}
			return UpstreamJobUpdate{}, settleErr
		}
		usage = settled
	}
	return s.finishGatewayResourceUsage(job, update, usage)
}

func (s *OpenAIGatewayService) finishGatewayResourceUsage(job *UpstreamJob, update UpstreamJobUpdate, usage GatewayResourceBatchUsage) (UpstreamJobUpdate, error) {
	encoded, err := GatewayResourceBatchUsageJSON(usage)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	update.Usage = encoded
	return update, nil
}

// settleOpenAIBatchResult downloads and validates the batch output file, then
// aggregates billable token usage. Provider counters must agree with the
// admitted request set: an incomplete or unverifiable result is never billed.
func (s *OpenAIGatewayService) settleOpenAIBatchResult(ctx context.Context, account *Account, job *UpstreamJob, update UpstreamJobUpdate, usage GatewayResourceBatchUsage) (GatewayResourceBatchUsage, error) {
	snapshot := job.Billing.ResourceBatch
	counts := usage.RequestCounts
	if snapshot == nil || counts == nil {
		return usage, ErrUpstreamJobUnsupportedUsage
	}
	total, totalOK := counts["total"]
	completed, completedOK := counts["completed"]
	failed, failedOK := counts["failed"]
	if !totalOK || !completedOK || !failedOK || completed+failed != total || total != int64(snapshot.RequestCount) {
		return usage, ErrUpstreamJobUnsupportedUsage
	}
	if completed == 0 {
		if failed != total {
			return usage, ErrUpstreamJobUnsupportedUsage
		}
		return usage, nil
	}
	if !openAIResourceID(update.ResultRef) {
		return usage, ErrUpstreamJobUnsupportedUsage
	}
	lines, matched, tokens, err := s.aggregateOpenAIBatchOutput(ctx, account, job, update.ResultRef)
	if err != nil {
		return usage, err
	}
	if lines != int(total) || matched != int(completed) {
		return usage, ErrUpstreamJobUnsupportedUsage
	}
	usage.Lines, usage.Matched = lines, matched
	usage.InputTokens, usage.OutputTokens, usage.CachedTokens = tokens.InputTokens, tokens.OutputTokens, tokens.CacheReadTokens
	return usage, nil
}

// aggregateOpenAIBatchOutput streams one output file and returns the billable
// token totals. Every line must map to an admitted request; a repeated, unknown
// or unverifiable line fails closed instead of being partially billed.
func (s *OpenAIGatewayService) aggregateOpenAIBatchOutput(ctx context.Context, account *Account, job *UpstreamJob, resourceID string) (int, int, UsageTokens, error) {
	snapshot := job.Billing.ResourceBatch
	response, err := s.openAIResourceStream(ctx, account, "/files/"+url.PathEscape(resourceID)+"/content")
	if err != nil {
		return 0, 0, UsageTokens{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return 0, 0, UsageTokens{}, fmt.Errorf("OpenAI batch output temporarily unavailable: %d", response.StatusCode)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return 0, 0, UsageTokens{}, ErrUpstreamJobUnsupportedUsage
	}
	expected := make(map[string]bool, len(snapshot.CustomIDHashes))
	for _, hashed := range snapshot.CustomIDHashes {
		expected[hashed] = true
	}
	bounded := &gatewayResourceBoundedReader{reader: response.Body, limit: GatewayResourceResultLimit}
	scanner := bufio.NewScanner(bounded)
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	lines, matched := 0, 0
	tokens := UsageTokens{}
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			return 0, 0, UsageTokens{}, ErrUpstreamJobUnsupportedUsage
		}
		if !gjson.ValidBytes(line) || !gjson.ParseBytes(line).IsObject() {
			return 0, 0, UsageTokens{}, ErrUpstreamJobUnsupportedUsage
		}
		row := gjson.ParseBytes(line)
		if row.Get("custom_id").Type != gjson.String {
			return 0, 0, UsageTokens{}, ErrUpstreamJobUnsupportedUsage
		}
		hashed := gatewayResourceHash([]byte(row.Get("custom_id").String()))
		if !expected[hashed] {
			return 0, 0, UsageTokens{}, ErrUpstreamJobUnsupportedUsage
		}
		delete(expected, hashed)
		lines++
		if lines > GatewayResourceBatchLineLimit {
			return 0, 0, UsageTokens{}, errGatewayResourceStreamTooLarge
		}
		if row.Get("error").Exists() && row.Get("error").Type != gjson.Null {
			continue
		}
		if row.Get("response.status_code").Int() != http.StatusOK {
			continue
		}
		lineTokens, ok := gatewayResourceOpenAIUsageTokens(snapshot.Endpoint, row.Get("response.body"))
		if !ok || row.Get("response.body.model").String() != snapshot.UpstreamModel {
			return 0, 0, UsageTokens{}, ErrUpstreamJobUnsupportedUsage
		}
		matched++
		tokens.InputTokens += lineTokens.InputTokens
		tokens.OutputTokens += lineTokens.OutputTokens
		tokens.CacheReadTokens += lineTokens.CacheReadTokens
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, UsageTokens{}, err
	}
	if len(expected) != 0 {
		return 0, 0, UsageTokens{}, ErrUpstreamJobUnsupportedUsage
	}
	return lines, matched, tokens, nil
}

// gatewayResourceOpenAIUsageTokens normalizes one batch output usage object
// through the same provider normalizer the synchronous path uses, then splits
// the mutually exclusive input buckets. Image usage and inconsistent buckets are
// refused instead of being partially priced.
func gatewayResourceOpenAIUsageTokens(endpoint string, body gjson.Result) (UsageTokens, bool) {
	if endpoint == "/v1/moderations" {
		// Moderation responses carry no token usage and are free upstream.
		return UsageTokens{}, true
	}
	if !gatewayResourceContains(gatewayResourceSettledBatchEndpoints, endpoint) {
		return UsageTokens{}, false
	}
	usage := body.Get("usage")
	if !usage.IsObject() {
		return UsageTokens{}, false
	}
	parsed, ok := openAIUsageFromGJSON(usage)
	if !ok || parsed.ImageInputTokens > 0 || parsed.ImageOutputTokens > 0 {
		return UsageTokens{}, false
	}
	if parsed.InputTokens <= 0 && parsed.OutputTokens <= 0 {
		return UsageTokens{}, false
	}
	uncachedInput := parsed.InputTokens - parsed.CacheReadInputTokens - parsed.CacheCreationInputTokens
	if uncachedInput < 0 {
		return UsageTokens{}, false
	}
	return UsageTokens{
		InputTokens:         uncachedInput,
		OutputTokens:        parsed.OutputTokens,
		CacheReadTokens:     parsed.CacheReadInputTokens,
		CacheCreationTokens: parsed.CacheCreationInputTokens,
	}, true
}

type gatewayResourceBoundedReader struct {
	reader io.Reader
	read   int64
	limit  int64
}

func (b *gatewayResourceBoundedReader) Read(buffer []byte) (int, error) {
	if b.read > b.limit {
		return 0, errGatewayResourceStreamTooLarge
	}
	count, err := b.reader.Read(buffer)
	b.read += int64(count)
	return count, err
}

// anthropicResourceAccount revalidates the account pinned at admission. A
// disabled or re-typed account must not keep serving a stored batch.
func (s *OpenAIGatewayService) anthropicResourceAccount(ctx context.Context, job *UpstreamJob, submit bool) (*Account, error) {
	if job == nil || job.Platform != PlatformAnthropic {
		return nil, ErrUpstreamJobNotFound
	}
	account, err := s.accountRepo.GetByID(ctx, job.AccountID)
	if err != nil {
		return nil, err
	}
	if account == nil || account.Platform != PlatformAnthropic || account.Type != AccountTypeAPIKey || !account.IsActive() {
		return nil, errors.New("Anthropic message batch account unavailable")
	}
	if submit && (job.GroupID == nil || !slices.Contains(account.GroupIDs, *job.GroupID)) {
		return nil, errors.New("Anthropic message batch account is outside the active group")
	}
	return account, nil
}

// buildAnthropicMessageBatchRequest mirrors the native message batch wire shape
// owned by the Anthropic action adapter. Results are rebuilt from the pinned
// account and task, never from a provider or client supplied URL.
func (s *OpenAIGatewayService) buildAnthropicMessageBatchRequest(ctx context.Context, account *Account, job *UpstreamJob, operation string, body []byte) (*http.Request, error) {
	if account == nil || job == nil || job.AccountID != account.ID || job.Platform != PlatformAnthropic || job.Family != "batch" {
		return nil, ErrUpstreamJobNotFound
	}
	if account.Platform != PlatformAnthropic || account.Type != AccountTypeAPIKey {
		return nil, errors.New("Anthropic message batch credentials are not verified for this account type")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	credential := strings.TrimSpace(account.GetCredential("api_key"))
	if credential == "" {
		return nil, errors.New("api_key not found in credentials")
	}
	method, suffix := http.MethodGet, ""
	switch operation {
	case "create":
		if job.UpstreamTaskID != "" {
			return nil, errors.New("Anthropic message batch already has an upstream binding")
		}
		if err := ValidateAnthropicMessageBatch(body); err != nil {
			return nil, err
		}
		method = http.MethodPost
	case "retrieve", "cancel", "results", "delete":
		if !strings.HasPrefix(job.UpstreamTaskID, "msgbatch_") || !anthropicBatchIdentifier.MatchString(job.UpstreamTaskID) {
			return nil, errors.New("invalid Anthropic message batch upstream binding")
		}
		suffix, body = "/"+job.UpstreamTaskID, nil
		switch operation {
		case "cancel":
			method, suffix = http.MethodPost, suffix+"/cancel"
		case "results":
			suffix += "/results"
		case "delete":
			method = http.MethodDelete
		}
	default:
		return nil, errors.New("unknown Anthropic message batch operation")
	}
	baseURL := account.GetBaseURL()
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	validated, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	target := strings.TrimSuffix(strings.TrimRight(validated, "/"), "/v1") + "/v1/messages/batches" + suffix
	request, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	setAnthropicAPIKeyAuthHeader(request.Header, account, credential, baseURL)
	request.Header.Set("anthropic-version", "2023-06-01")
	request.Header.Set("Content-Type", "application/json")
	account.ApplyHeaderOverrides(request.Header)
	return request, nil
}

func (s *OpenAIGatewayService) doAnthropicMessageBatch(ctx context.Context, account *Account, job *UpstreamJob, operation string, body []byte) (*http.Response, error) {
	request, err := s.buildAnthropicMessageBatchRequest(ctx, account, job, operation, body)
	if err != nil {
		return nil, err
	}
	return s.doOpenAIUpstream(request, gatewayResourceProxyURL(account), account)
}

func gatewayResourceAnthropicBody(response *http.Response) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// SubmitAnthropicMessageBatch creates one native message batch from an
// already-admitted job.
func (s *OpenAIGatewayService) SubmitAnthropicMessageBatch(ctx context.Context, account *Account, job *UpstreamJob, body []byte) (UpstreamJobUpdate, error) {
	response, err := s.doAnthropicMessageBatch(ctx, account, job, "create", body)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := gatewayResourceAnthropicBody(response)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return gatewayResourceAnthropicFailure(response.StatusCode, raw), nil
	}
	state, err := ParseAnthropicMessageBatchState(raw, "")
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	// An immediately ended batch is still polled: settlement requires the result
	// file to be matched against the admitted requests.
	return UpstreamJobUpdate{TaskID: state.ID, Status: "running", ProviderStatus: state.ProcessingStatus, Usage: gatewayResourceAnthropicUsage(state, GatewayResourceBatchUsage{})}, nil
}

func gatewayResourceAnthropicCounts(state *AnthropicMessageBatchState) map[string]int64 {
	if state == nil {
		return nil
	}
	return map[string]int64{
		"processing": int64(state.RequestCounts.Processing),
		"succeeded":  int64(state.RequestCounts.Succeeded),
		"errored":    int64(state.RequestCounts.Errored),
		"canceled":   int64(state.RequestCounts.Canceled),
		"expired":    int64(state.RequestCounts.Expired),
	}
}

func gatewayResourceAnthropicUsage(state *AnthropicMessageBatchState, usage GatewayResourceBatchUsage) json.RawMessage {
	if usage.RequestCounts == nil {
		usage.RequestCounts = gatewayResourceAnthropicCounts(state)
	}
	encoded, err := GatewayResourceBatchUsageJSON(usage)
	if err != nil {
		return nil
	}
	return encoded
}

func gatewayResourceAnthropicFailure(status int, raw []byte) UpstreamJobUpdate {
	message := strings.TrimSpace(gjson.GetBytes(raw, "error.message").String())
	reason := fmt.Sprintf("Anthropic rejected the request (HTTP %d)", status)
	if message != "" {
		reason += ": " + message
	}
	return UpstreamJobUpdate{Status: "failed", ErrorCode: "upstream_rejected", ErrorMessage: sanitizeUpstreamErrorMessage(reason)}
}

// pollGatewayResourceAnthropicBatch refreshes one native message batch. "ended"
// only means processing stopped: results are downloaded and every response is
// matched to an admitted request before the shared job service settles it.
func (s *OpenAIGatewayService) pollGatewayResourceAnthropicBatch(ctx context.Context, job *UpstreamJob) (UpstreamJobUpdate, error) {
	account, err := s.anthropicResourceAccount(ctx, job, false)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	response, err := s.doAnthropicMessageBatch(ctx, account, job, "retrieve", nil)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := gatewayResourceAnthropicBody(response)
	if err != nil {
		return UpstreamJobUpdate{}, err
	}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return UpstreamJobUpdate{}, fmt.Errorf("Anthropic batch state temporarily unavailable: %d", response.StatusCode)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return gatewayResourceAnthropicFailure(response.StatusCode, raw), nil
	}
	state, err := ParseAnthropicMessageBatchState(raw, job.UpstreamTaskID)
	if err != nil {
		return UpstreamJobUpdate{TaskID: job.UpstreamTaskID, Status: "failed", ErrorCode: "invalid_upstream_response", ErrorMessage: "Unrecognized Anthropic batch state"}, nil
	}
	update := UpstreamJobUpdate{TaskID: state.ID, ProviderStatus: state.ProcessingStatus}
	switch state.ProcessingStatus {
	case "in_progress", "canceling":
		update.Status = "running"
		return s.finishGatewayResourceUsage(job, update, GatewayResourceBatchUsage{RequestCounts: gatewayResourceAnthropicCounts(state), ProviderStatus: state.ProcessingStatus})
	case "ended":
		usage := GatewayResourceBatchUsage{RequestCounts: gatewayResourceAnthropicCounts(state), ProviderStatus: state.ProcessingStatus}
		settled, settleErr := s.settleAnthropicMessageBatchResult(ctx, account, job, usage)
		if settleErr != nil {
			if errors.Is(settleErr, ErrUpstreamJobUnsupportedUsage) || errors.Is(settleErr, errGatewayResourceStreamTooLarge) {
				update.Status = "failed"
				update.ErrorCode = "unsupported_usage"
				update.ErrorMessage = "Batch results cannot be priced; evidence retained for review"
				return s.finishGatewayResourceUsage(job, update, usage)
			}
			return UpstreamJobUpdate{}, settleErr
		}
		update.Status = "succeeded"
		return s.finishGatewayResourceUsage(job, update, settled)
	default:
		return UpstreamJobUpdate{TaskID: job.UpstreamTaskID, Status: "failed", ErrorCode: "invalid_upstream_response", ErrorMessage: "Unrecognized Anthropic batch state"}, nil
	}
}

func (s *OpenAIGatewayService) settleAnthropicMessageBatchResult(ctx context.Context, account *Account, job *UpstreamJob, usage GatewayResourceBatchUsage) (GatewayResourceBatchUsage, error) {
	snapshot := job.Billing.ResourceBatch
	if snapshot == nil || usage.RequestCounts == nil {
		return usage, ErrUpstreamJobUnsupportedUsage
	}
	admitted := int64(0)
	for _, count := range usage.RequestCounts {
		admitted += count
	}
	if usage.RequestCounts["processing"] != 0 || admitted != int64(snapshot.RequestCount) {
		return usage, ErrUpstreamJobUnsupportedUsage
	}
	response, err := s.doAnthropicMessageBatch(ctx, account, job, "results", nil)
	if err != nil {
		return usage, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return usage, fmt.Errorf("Anthropic batch results temporarily unavailable: %d", response.StatusCode)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return usage, ErrUpstreamJobUnsupportedUsage
	}
	expected := make(map[string]bool, len(snapshot.CustomIDHashes))
	for _, hashed := range snapshot.CustomIDHashes {
		expected[hashed] = true
	}
	bounded := &gatewayResourceBoundedReader{reader: response.Body, limit: GatewayResourceResultLimit}
	scanner := bufio.NewScanner(bounded)
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	lines, matched := 0, 0
	tokens := UsageTokens{}
	succeeded := usage.RequestCounts["succeeded"]
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			return usage, ErrUpstreamJobUnsupportedUsage
		}
		result, parseErr := ParseAnthropicMessageBatchResult(line)
		if parseErr != nil {
			return usage, ErrUpstreamJobUnsupportedUsage
		}
		hashed := gatewayResourceHash([]byte(result.CustomID))
		if !expected[hashed] {
			return usage, ErrUpstreamJobUnsupportedUsage
		}
		delete(expected, hashed)
		lines++
		if lines > GatewayResourceBatchLineLimit {
			return usage, errGatewayResourceStreamTooLarge
		}
		if result.Status != "succeeded" {
			continue
		}
		if result.Model != snapshot.UpstreamModel || result.Usage == nil || gatewayResourceMessageUsesServerTools(result.Message) {
			return usage, ErrUpstreamJobUnsupportedUsage
		}
		matched++
		tokens.InputTokens += result.Usage.InputTokens
		tokens.OutputTokens += result.Usage.OutputTokens
		tokens.CacheCreationTokens += result.Usage.CacheCreationInputTokens
		tokens.CacheReadTokens += result.Usage.CacheReadInputTokens
		tokens.CacheCreation5mTokens += result.Usage.CacheCreation5mTokens
		tokens.CacheCreation1hTokens += result.Usage.CacheCreation1hTokens
	}
	if err := scanner.Err(); err != nil {
		return usage, err
	}
	if len(expected) != 0 || int64(matched) != succeeded {
		return usage, ErrUpstreamJobUnsupportedUsage
	}
	usage.Lines, usage.Matched = lines, matched
	usage.InputTokens, usage.OutputTokens = tokens.InputTokens, tokens.OutputTokens
	usage.CacheCreation, usage.CacheCreation5m, usage.CacheCreation1h = tokens.CacheCreationTokens, tokens.CacheCreation5mTokens, tokens.CacheCreation1hTokens
	usage.CacheRead = tokens.CacheReadTokens
	return usage, nil
}

// gatewayResourceMessageUsesServerTools reports server side tool usage, which
// carries fees outside the token tariff and must not be settled by token math.
func gatewayResourceMessageUsesServerTools(message json.RawMessage) bool {
	types := gjson.GetBytes(message, "content.#.type")
	if !types.IsArray() {
		return false
	}
	for _, entry := range types.Array() {
		if strings.Contains(entry.String(), "server_tool_use") || strings.Contains(entry.String(), "web_search_tool_result") {
			return true
		}
	}
	return false
}

// OpenAnthropicMessageBatchResults streams the native result file of one ended
// batch. The caller bounds the client-facing copy.
func (s *OpenAIGatewayService) OpenAnthropicMessageBatchResults(ctx context.Context, job *UpstreamJob) (*http.Response, error) {
	account, err := s.anthropicResourceAccount(ctx, job, false)
	if err != nil {
		return nil, err
	}
	return s.doAnthropicMessageBatch(ctx, account, job, "results", nil)
}

// ManageAnthropicResource applies one explicit message batch transition.
func (s *OpenAIGatewayService) ManageAnthropicResource(ctx context.Context, job *UpstreamJob, action string) error {
	account, err := s.anthropicResourceAccount(ctx, job, false)
	if err != nil {
		return err
	}
	operation := ""
	switch action {
	case "cancel":
		operation = "cancel"
	case "delete":
		operation = "delete"
	default:
		return ErrUpstreamResourceConflict
	}
	response, err := s.doAnthropicMessageBatch(ctx, account, job, operation, nil)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := gatewayResourceAnthropicBody(response)
	if err != nil {
		return err
	}
	if response.StatusCode == http.StatusNotFound && action == "delete" {
		// Retry after a local rollback of an already applied remote deletion.
		return nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("resource operation rejected upstream")
	}
	if action == "cancel" {
		switch gjson.GetBytes(raw, "processing_status").String() {
		case "canceling", "ended":
			return nil
		}
		return errors.New("invalid cancellation response")
	}
	if gjson.GetBytes(raw, "id").String() != job.UpstreamTaskID {
		return errors.New("invalid deletion response")
	}
	return nil
}

// ForwardOpenAIModeration executes one native moderation request. The endpoint is
// free upstream, so the shared layer records no token charge for it.
func (s *OpenAIGatewayService) ForwardOpenAIModeration(ctx context.Context, account *Account, body []byte) (*http.Response, error) {
	credential, err := openAIActionCredential(account)
	if err != nil {
		return nil, err
	}
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return nil, errors.New("moderation body must be a JSON object")
	}
	input := gjson.GetBytes(body, "input")
	if !input.Exists() || input.Type == gjson.Null {
		return nil, errors.New("moderation input is required")
	}
	target, err := s.openAIActionURL(account, "/moderations")
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	account.ApplyHeaderOverrides(request.Header)
	request.Header.Set("Authorization", "Bearer "+credential)
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(WithHTTPUpstreamProfile(request.Context(), HTTPUpstreamProfileOpenAI))
	return s.doOpenAIUpstream(request, gatewayResourceProxyURL(account), account)
}
