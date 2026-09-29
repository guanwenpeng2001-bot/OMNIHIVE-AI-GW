package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/requestmodel"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/tidwall/gjson"
)

var ErrAccountModelUnsupported = errors.New("model is not supported by the selected account")

func (s *GatewayService) doModelCheckedUpstream(request *http.Request, proxyURL string, account *Account, model string, profile *tlsfingerprint.Profile) (*http.Response, error) {
	if err := validateOpenAIForwardRequest(s.accountRepo, account, request); err != nil {
		return nil, err
	}
	return generationDoWithTLS(s.httpUpstream, request, proxyURL, account.ID, account.Concurrency, profile)
}

func (s *OpenAIGatewayService) modelBindingCredentialAccount(ctx context.Context, account *Account) *Account {
	if account == nil || !account.IsShadow() || s == nil || s.accountRepo == nil {
		return nil
	}
	parent, _ := loadCatalogCredentialAccount(ctx, s.accountRepo, account)
	return parent
}

func loadCatalogCredentialAccount(ctx context.Context, repo AccountRepository, account *Account) (*Account, error) {
	if account == nil || !account.IsShadow() || repo == nil {
		return nil, nil
	}
	if reader, ok := repo.(ModelCatalogAccountRepository); ok {
		return reader.GetModelCatalogAccount(ctx, *account.ParentAccountID)
	}
	return repo.GetByID(ctx, *account.ParentAccountID)
}

// openAICompatiblePlanCatalogEndpoint reports the catalog endpoint family a
// planned OpenAI-compatible request is judged on. Media families read the
// endpoint their protocol actually executes on; chat-only audio and rerank
// actions likewise require their own family. Text keeps its existing bridge
// rule. Scheduling, pre-profit eligibility and affinity revocation all read
// this same rule.
func openAICompatiblePlanCatalogEndpoint(ctx context.Context, account *Account) string {
	plan := GatewayRequestPlanFromContext(ctx)
	if plan == nil {
		return ""
	}
	switch plan.Snapshot().Protocol {
	case "media":
		// Media evidence is recorded per platform: Grok declares its imagine
		// family, other OpenAI-compatible platforms declare image endpoints for
		// image and video tasks (account_model_catalog.go).
		if account != nil && account.IsGrok() {
			return requestCatalogEndpoint(ctx)
		}
		// Non-text tasks with explicit media evidence declare the media family,
		// including chat-only platforms. Images and videos both execute here.
		return CompositeRouteEndpointImages
	case "audio", "rerank":
		if account != nil && IsChatOnlyPlatform(account.Platform) {
			if plan.Snapshot().Protocol == "rerank" {
				return "rerank"
			}
			// Use the same endpoint-to-action mapping as catalog declarations.
			// gatewayRequiredAction also resolves native routes when Action is absent.
			action := gatewayRequiredAction(ctx)
			for _, endpoint := range []string{"speech", "transcriptions", "voice"} {
				if chatOnlyCatalogEndpointAction(endpoint) == action {
					return endpoint
				}
			}
			// An unresolved audio direction cannot borrow text evidence.
			return "audio"
		}
		// Other platforms retain their existing audio/bridge catalog contract.
		return CompositeRouteEndpointResponses
	case "images", "embeddings", "search", "live", "realtime":
		return requestCatalogEndpoint(ctx)
	default:
		// A chat-only entry is by definition an OpenAI-shaped chat trunk. When the
		// platform publishes no native text protocol at all (no Responses and no
		// Messages), its text models can only declare the chat-completions family
		// (account_model_catalog.go). Measuring such an account against the
		// conservative Responses default asks it to prove evidence it never
		// declares, which locks the entry out of every text request. Platforms that
		// do publish a native text protocol keep the Responses default and its
		// bridge.
		if account != nil && IsChatOnlyPlatform(account.Platform) && len(chatOnlyPublishedTextFamilies(account)) == 0 {
			return CompositeRouteEndpointChatCompletions
		}
		return CompositeRouteEndpointResponses
	}
}

func (s *OpenAIGatewayService) supportsOpenAIAccountModel(ctx context.Context, account *Account, model string, capability OpenAIEndpointCapability) bool {
	endpoint := string(capability)
	if GatewayRequestPlanFromContext(ctx) != nil {
		endpoint = openAICompatiblePlanCatalogEndpoint(ctx, account)
	}
	options := AccountModelBindingOptions{Endpoint: endpoint}
	if account != nil && account.IsShadow() {
		options.CredentialAccount = s.modelBindingCredentialAccount(ctx, account)
	}
	return AccountSupportsResolvedModel(ctx, account, model, options)
}

func validateOpenAIForwardRequest(repo AccountRepository, account *Account, request *http.Request) error {
	if GatewayRequestPlanFromContext(request.Context()) == nil {
		return nil
	}
	if request.Method != http.MethodPost {
		return nil
	}
	endpoint := ""
	path := strings.TrimRight(request.URL.Path, "/")
	switch {
	case strings.HasSuffix(path, ":rawPredict"), strings.HasSuffix(path, ":streamRawPredict"):
		endpoint = "messages"
	case account.Platform == PlatformQianwen && strings.HasSuffix(path, "/generation"):
		endpoint = "chat_completions"
	case strings.HasSuffix(path, "/responses/compact"):
		endpoint = "responses/compact"
	case strings.HasSuffix(path, "/responses"), strings.HasSuffix(path, "/responses/input_tokens"):
		endpoint = "responses"
	case strings.HasSuffix(path, "/chat/completions"):
		endpoint = "chat_completions"
	case strings.HasSuffix(path, "/messages"), strings.HasSuffix(path, "/messages/count_tokens"):
		endpoint = "messages"
	case strings.HasSuffix(path, "/images/generations"), strings.HasSuffix(path, "/images/edits"):
		endpoint = CompositeRouteEndpointImages
	case strings.HasSuffix(path, "/alpha/search"):
		endpoint = string(OpenAIEndpointCapabilityAlphaSearch)
	case strings.HasSuffix(path, "/embeddings"):
		endpoint = "embeddings"
	default:
		// Other protocols validate at their own resolved-model boundary.
		return nil
	}
	if request.GetBody == nil {
		return ErrAccountModelUnsupported
	}
	body, err := request.GetBody()
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	if mediaType, _, _ := mime.ParseMediaType(request.Header.Get("Content-Type")); mediaType != "multipart/form-data" {
		if err := requestmodel.ValidateJSON(data); err != nil {
			return err
		}
	}
	model := gjson.GetBytes(data, "model").String()
	if strings.HasSuffix(path, ":rawPredict") || strings.HasSuffix(path, ":streamRawPredict") {
		_, suffix, _ := strings.Cut(path, "/models/")
		model, _, _ = strings.Cut(suffix, ":")
	}
	if endpoint == CompositeRouteEndpointImages {
		mediaType, params, _ := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if mediaType == "multipart/form-data" {
			reader := multipart.NewReader(strings.NewReader(string(data)), params["boundary"])
			for {
				part, partErr := reader.NextPart()
				if partErr == io.EOF {
					break
				}
				if partErr != nil {
					return partErr
				}
				if part.FormName() == "model" {
					value, readErr := io.ReadAll(part)
					if readErr != nil {
						return readErr
					}
					model = string(value)
				}
			}
		}
	}
	return validateAccountForwardModel(request.Context(), repo, account, model, endpoint)
}

// validateAccountForwardModel checks the actual wire target without applying an
// alias a second time. Read current evidence at the send boundary so a stale
// scheduler or sticky entry cannot retain revoked membership.
func validateAccountForwardModel(ctx context.Context, repo AccountRepository, account *Account, finalModel, endpoint string) error {
	if GatewayRequestPlanFromContext(ctx) == nil {
		return nil
	}
	if account == nil || finalModel == "" {
		return ErrAccountModelUnsupported
	}
	current := account
	if reader, ok := repo.(ModelCatalogAccountRepository); ok {
		var err error
		current, err = reader.GetModelCatalogAccount(ctx, account.ID)
		if err != nil {
			return err
		}
		if current == nil || current.Platform != account.Platform || current.ModelCatalogState().IdentityRevision != account.ModelCatalogState().IdentityRevision || current.ModelCatalogState().RulesRevision != account.ModelCatalogState().RulesRevision {
			return ErrAccountModelUnsupported
		}
	}
	options := AccountModelBindingOptions{FinalModel: finalModel, Endpoint: endpoint}
	if endpoint == "responses/compact" {
		options.Endpoint = CompositeRouteEndpointResponses
		options.Compact = true
	}
	if current.IsShadow() {
		var err error
		if reader, ok := repo.(ModelCatalogAccountRepository); ok {
			options.CredentialAccount, err = reader.GetModelCatalogAccount(ctx, *current.ParentAccountID)
		} else if repo != nil {
			options.CredentialAccount, err = repo.GetByID(ctx, *current.ParentAccountID)
		}
		if err != nil {
			return err
		}
	}
	publicModel := finalModel
	if requested := GatewayRequestPlanFromContext(ctx).Snapshot().Model; requested != "" {
		publicModel = requested
	}
	if !AccountSupportsResolvedModel(ctx, current, publicModel, options) {
		return ErrAccountModelUnsupported
	}
	return admitGatewayRequestBinding(ctx, account.ID)
}

// doCatalogCheckedGeneration validates the exact serialized Gemini/AG target on
// every retry. Reading GetBody leaves the outgoing message prefix untouched.
func doCatalogCheckedGeneration(upstream HTTPUpstream, repo AccountRepository, request *http.Request, proxyURL string, account *Account) (*http.Response, error) {
	if GatewayRequestPlanFromContext(request.Context()) != nil {
		model := ""
		if request.GetBody != nil {
			body, err := request.GetBody()
			if err != nil {
				return nil, err
			}
			data, err := io.ReadAll(body)
			_ = body.Close()
			if err != nil {
				return nil, err
			}
			model = gjson.GetBytes(data, "model").String()
		}
		if model == "" {
			if _, suffix, ok := strings.Cut(request.URL.Path, "/models/"); ok {
				model, _, _ = strings.Cut(suffix, ":")
			}
		}
		if err := validateAccountForwardModel(request.Context(), repo, account, model, "generate_content"); err != nil {
			return nil, err
		}
	}
	return generationDo(upstream, request, proxyURL, account.ID, account.Concurrency)
}
func requestCatalogEndpoint(ctx context.Context) string {
	plan := GatewayRequestPlanFromContext(ctx)
	if plan == nil {
		return ""
	}
	switch plan.Snapshot().Protocol {
	case "anthropic":
		return "messages"
	case "responses":
		return "responses"
	case "gemini":
		return "generate_content"
	case "images":
		return CompositeRouteEndpointImages
	case "embeddings":
		return string(OpenAIEndpointCapabilityEmbeddings)
	case "media":
		return string(OpenAIEndpointCapabilityGrokMediaGeneration)
	case "search":
		return string(OpenAIEndpointCapabilityAlphaSearch)
	case "live", "realtime":
		return string(OpenAIEndpointCapabilityLive)
	default:
		return "chat_completions"
	}
}

// CatalogModelIDs returns nil only for legacy repository adapters. A production
// catalog reader returns an authoritative (possibly empty) set, never defaults.
func (s *GatewayService) CatalogModelIDs(ctx context.Context, groupID *int64, platform string, mixedOnly ...bool) ([]string, error) {
	return s.catalogModelListing(ctx, groupID, platform, nil, mixedOnly...)
}

// CatalogModelsWithMetadata projects specifications from the same account read
// and bindings as membership. Metadata is never an input to eligibility.
func (s *GatewayService) CatalogModelsWithMetadata(ctx context.Context, groupID *int64, platform string) ([]string, map[string][]UpstreamModelMetadata, error) {
	view, metadata, err := s.VisibleModelsWithMetadata(ctx, groupID, platform)
	return view.Models, metadata, err
}

func (s *GatewayService) catalogModelListing(ctx context.Context, groupID *int64, platform string, metadata map[string][]UpstreamModelMetadata, mixedOnly ...bool) ([]string, error) {
	result, err := (&VisibleModelResolver{gateway: s}).resolve(ctx, groupID, platform, metadata, mixedOnly...)
	return result.Models, err
}

func filterCatalogModelIDs(ctx context.Context, repo AccountRepository, accounts []Account, ids []string, endpoint string) []string {
	if _, ok := repo.(ModelCatalogAccountRepository); !ok {
		return ids
	}
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		for i := range accounts {
			account := &accounts[i]
			parent, err := loadCatalogCredentialAccount(ctx, repo, account)
			if err != nil {
				continue
			}
			if AccountSupportsResolvedModel(ctx, account, id, AccountModelBindingOptions{Endpoint: endpoint, CredentialAccount: parent}) {
				result = append(result, id)
				break
			}
		}
	}
	return result
}
func filterCatalogModelsBody(ctx context.Context, repo AccountRepository, account *Account, body []byte, codex bool) ([]byte, error) {
	if _, ok := repo.(ModelCatalogAccountRepository); !ok {
		return body, nil
	}
	field, idField := "data", "id"
	if codex {
		field, idField = "models", "slug"
	}
	envelope, entries, err := modelCatalogEntries(body, field)
	if err != nil {
		return nil, err
	}
	parent, err := loadCatalogCredentialAccount(ctx, repo, account)
	if err != nil {
		return nil, err
	}
	filtered := make([]json.RawMessage, 0, len(entries))
	for _, entry := range entries {
		id := gjson.GetBytes(entry, idField).String()
		if AccountSupportsResolvedModel(ctx, account, id, AccountModelBindingOptions{CredentialAccount: parent}) {
			filtered = append(filtered, entry)
		}
	}
	envelope[field], err = json.Marshal(filtered)
	if err != nil {
		return nil, err
	}
	return json.Marshal(envelope)
}

func requestAccountCatalogSupports(ctx context.Context, account *Account, model, endpoint string) bool {
	plan := GatewayRequestPlanFromContext(ctx)
	if plan == nil {
		return true
	}
	parent, err := loadCatalogCredentialAccount(ctx, plan.catalogRepo, account)
	if err != nil {
		return false
	}
	return AccountSupportsResolvedModel(ctx, account, model, AccountModelBindingOptions{Endpoint: endpoint, CredentialAccount: parent})
}

func gatewaySelectionCatalogEndpoint(ctx context.Context, account *Account) string {
	if account != nil {
		if account.Platform == PlatformGemini || account.Platform == PlatformAntigravity {
			return "generate_content"
		}
		if account.Platform == PlatformCursor && account.Type != AccountTypeUpstream {
			return "chat_completions"
		}
	}
	return requestCatalogEndpoint(ctx)
}
