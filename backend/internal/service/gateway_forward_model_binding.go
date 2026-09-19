package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"sort"
	"strings"

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

func (s *OpenAIGatewayService) supportsOpenAIAccountModel(ctx context.Context, account *Account, model string, capability OpenAIEndpointCapability) bool {
	endpoint := string(capability)
	if GatewayRequestPlanFromContext(ctx) != nil {
		endpoint = CompositeRouteEndpointResponses
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
	if !AccountSupportsResolvedModel(ctx, current, finalModel, options) {
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
	metadata := make(map[string][]UpstreamModelMetadata)
	models, err := s.catalogModelListing(ctx, groupID, platform, metadata)
	return models, metadata, err
}

func (s *GatewayService) catalogModelListing(ctx context.Context, groupID *int64, platform string, metadata map[string][]UpstreamModelMetadata, mixedOnly ...bool) ([]string, error) {
	if s == nil {
		return nil, nil
	}
	reader, ok := s.accountRepo.(ModelCatalogAccountRepository)
	if !ok {
		return nil, nil
	}
	platforms := []string{platform}
	if platform == "" || platform == PlatformComposite {
		platforms = nil
	}
	accounts, err := reader.ListModelCatalogAccounts(ctx, groupID, platforms, s.cfg != nil && s.cfg.RunMode == "simple")
	if err != nil {
		return []string{}, err
	}
	candidates := make(map[string]struct{})
	for i := range accounts {
		account := &accounts[i]
		if len(mixedOnly) > 0 && mixedOnly[0] && account.Platform == PlatformAntigravity && !account.IsMixedSchedulingEnabled() {
			continue
		}
		parent, err := loadCatalogCredentialAccount(ctx, s.accountRepo, account)
		if err != nil {
			continue
		}
		account.modelCatalogCredentialAccount = parent
		catalog := account.EffectiveUpstreamModelCatalogForCredentialAccount(parent)
		if catalog == nil {
			continue
		}
		for _, member := range catalog.EligibleModels {
			candidates[member.ID] = struct{}{}
		}
		for alias := range account.GetModelMapping() {
			if !strings.Contains(alias, "*") {
				candidates[alias] = struct{}{}
			}
		}
	}
	var group *Group
	if groupID != nil && s.groupRepo != nil {
		group, err = s.groupRepo.GetByIDLite(ctx, *groupID)
		if err != nil {
			return []string{}, err
		}
	}
	if group != nil {
		for _, model := range group.ModelAllowlist.Models {
			if !strings.Contains(model, "*") {
				candidates[model] = struct{}{}
			}
		}
	}
	if groupID != nil && s.channelService != nil {
		channels, err := s.channelService.loadCache(ctx)
		if err != nil {
			return []string{}, err
		}
		for key := range channels.mappingByGroupModel {
			if key.groupID == *groupID && (platform == PlatformComposite || platform == "" || key.platform == platform) {
				candidates[key.model] = struct{}{}
			}
		}
	}
	resolver := s.compositeResolver
	if platform == PlatformComposite {
		if groupID == nil {
			return []string{}, nil
		}
		if resolver == nil {
			resolver = &CompositeRouteResolver{modelOwnershipResolver: s.resolveCompositeModelOwnership}
		}
		if resolver.repo != nil {
			routes, err := resolver.repo.ListByGroup(ctx, *groupID, false)
			if err != nil {
				return []string{}, err
			}
			copy := *resolver
			copy.repo = gatewayCatalogRoutes{CompositeModelRouteRepository: resolver.repo, routes: routes}
			resolver = &copy
			for _, route := range routes {
				if route.MatchType == CompositeRouteMatchExact {
					candidates[route.PublicModel] = struct{}{}
				}
			}
		}
	}
	result := make([]string, 0, len(candidates))
	// Decode each account's stored specification snapshot at most once. Reads
	// never refresh upstream facts, parse public registries, or persist anything.
	var specifications map[int64]*UpstreamModelMetadataSnapshot
	var profiles map[int64]string
	if metadata != nil {
		specifications = make(map[int64]*UpstreamModelMetadataSnapshot, len(accounts))
		profiles = make(map[int64]string, len(accounts))
		for index := range accounts {
			account := &accounts[index]
			if !account.IsShadow() {
				specifications[account.ID] = account.GetUpstreamModelMetadataSnapshot()
				profiles[account.ID] = resolveCapabilityProfile(account)
			}
		}
	}
	for model := range candidates {
		if group != nil && group.ModelAllowlistEnabled() && !group.ModelAllowlist.Allows(model) {
			continue
		}
		supported := false
		for _, endpoint := range []string{CompositeRouteEndpointResponses, CompositeRouteEndpointChatCompletions, CompositeRouteEndpointMessages, CompositeRouteEndpointGemini, CompositeRouteEndpointEmbeddings, CompositeRouteEndpointImages, string(OpenAIEndpointCapabilityLive)} {
			candidateCtx := ctx
			if platform == PlatformComposite {
				decision, err := resolver.Resolve(ctx, *groupID, model, endpoint)
				if err != nil {
					return []string{}, err
				}
				if !decision.Matched {
					continue
				}
				candidateCtx = WithCompositeRouteDecision(ctx, decision)
			}
			lookupModel := model
			if resolved, ok := ResolvedUpstreamModelFromContext(candidateCtx); ok {
				lookupModel = resolved
			}
			if s.channelService != nil && groupID != nil {
				mapping := s.channelService.ResolveChannelMapping(candidateCtx, *groupID, lookupModel)
				if mapping.Mapped {
					candidateCtx = WithOpenAIForwardModel(candidateCtx, mapping.MappedModel, false)
				}
			}
			for i := range accounts {
				account := &accounts[i]
				if len(mixedOnly) > 0 && mixedOnly[0] && account.Platform == PlatformAntigravity && !account.IsMixedSchedulingEnabled() {
					continue
				}
				options := AccountModelBindingOptions{Endpoint: endpoint, CredentialAccount: account.modelCatalogCredentialAccount}
				if group != nil && endpoint == CompositeRouteEndpointMessages {
					options.MessagesDispatchModel = group.ResolveMessagesDispatchModel(model)
				}
				binding := ResolveAccountModelBinding(candidateCtx, account, model, options)
				if binding.Supported {
					supported = true
					if metadata == nil {
						break
					}
					// Unknown facts on an exit for this listing endpoint remain unknown;
					// the handler intersects only an explicit safe field whitelist.
					projection := UpstreamModelMetadata{}
					if snapshot := specifications[account.ID]; snapshot != nil {
						candidate := snapshot.Models[binding.UpstreamModel]
						if candidate.ProviderProfile != "" && candidate.ProviderProfile == profiles[account.ID] {
							projection = candidate
						}
					}
					metadata[model] = append(metadata[model], projection)
				}
			}
			if supported {
				break
			}
		}
		if supported {
			result = append(result, model)
		}
	}
	sort.Strings(result)
	return result, nil
}

// Reuse the existing resolver against this projection's single route snapshot.
type gatewayCatalogRoutes struct {
	CompositeModelRouteRepository
	routes []CompositeModelRoute
}

func (r gatewayCatalogRoutes) ListByGroup(context.Context, int64, bool) ([]CompositeModelRoute, error) {
	return r.routes, nil
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
