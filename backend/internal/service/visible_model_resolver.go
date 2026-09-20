package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// VisibleModelSet is the finite, authorized public projection. Bindings are
// administration-only evidence: never serialize deployment IDs or credentials.
// FullyEnumerable=false means wildcard routing may accept additional concrete
// names; every such request must still authorize its public name and rebind.
type VisibleModelSet struct {
	Models          []string
	Revision        string
	FullyEnumerable bool
	Bindings        map[string][]AccountModelBinding `json:"-"`
}

// VisibleModelResolver is shared by gateway lists, plaza and channel views.
// Registry candidates and pricing are never membership evidence.
type VisibleModelResolver struct{ gateway *GatewayService }

type visibleModelPolicyKey struct{}

// WithVisibleModelKeyPolicy freezes the authenticated Key policy for this
// request. nil inherits; a non-nil empty array denies all. Persistence/auth
// adapters must preserve that distinction when attaching the policy.
func WithVisibleModelKeyPolicy(ctx context.Context, models []string) context.Context {
	var policy []string
	if models != nil {
		policy = append([]string{}, models...)
	}
	return context.WithValue(ctx, visibleModelPolicyKey{}, policy)
}

func visibleModelPolicyAllows(ctx context.Context, model string) bool {
	if ctx == nil {
		return true
	}
	policy, _ := ctx.Value(visibleModelPolicyKey{}).([]string)
	if policy == nil {
		return true
	}
	if strings.TrimSpace(model) == "" {
		return false
	}
	return (GroupModelAllowlist{Enabled: true, Models: policy}).Allows(model)
}

var ErrVisibleModelRevisionChanged = errors.New("visible model catalog changed during resolution")

type visibleModelRevisionKey struct{}
type visibleModelRevision struct {
	generation    int64
	authoritative bool
}

func (r *VisibleModelResolver) freeze(ctx context.Context) (context.Context, error) {
	if r == nil || r.gateway == nil {
		return ctx, nil
	}
	if _, ok := r.gateway.accountRepo.(ModelCatalogAccountRepository); !ok {
		return ctx, nil
	}
	if _, ok := ctx.Value(visibleModelRevisionKey{}).(visibleModelRevision); ok {
		return ctx, nil
	}
	generation, authoritative, err := modelCatalogGeneration(ctx, r.gateway.accountRepo, "catalog")
	if err != nil {
		return ctx, err
	}
	ctx = WithModelRegistrySnapshot(ctx, ModelRegistrySnapshotFromContext(ctx))
	return context.WithValue(ctx, visibleModelRevisionKey{}, visibleModelRevision{generation, authoritative}), nil
}

func (r *VisibleModelResolver) validate(ctx context.Context) error {
	if r == nil || r.gateway == nil {
		return nil
	}
	revision, ok := ctx.Value(visibleModelRevisionKey{}).(visibleModelRevision)
	if !ok || !revision.authoritative {
		return nil
	}
	generation, _, err := modelCatalogGeneration(ctx, r.gateway.accountRepo, "catalog")
	if err != nil {
		return err
	}
	if generation != revision.generation {
		return ErrVisibleModelRevisionChanged
	}
	return nil
}

func (r *VisibleModelResolver) Resolve(ctx context.Context, groupID *int64, platform string) (VisibleModelSet, error) {
	return r.resolve(ctx, groupID, platform, nil)
}

func (s *GatewayService) VisibleModels(ctx context.Context, groupID *int64, platform string) (VisibleModelSet, error) {
	return (&VisibleModelResolver{gateway: s}).Resolve(ctx, groupID, platform)
}

// VisibleModelsWithMetadata returns safe-membership evidence and raw internal
// specifications from the same read/revision. Protocol serializers must project
// their public field whitelist; these specifications are not a public DTO.
func (s *GatewayService) VisibleModelsWithMetadata(ctx context.Context, groupID *int64, platform string) (VisibleModelSet, map[string][]UpstreamModelMetadata, error) {
	metadata := make(map[string][]UpstreamModelMetadata)
	view, err := (&VisibleModelResolver{gateway: s}).resolve(ctx, groupID, platform, metadata)
	if err != nil {
		return VisibleModelSet{}, nil, err
	}
	return view, metadata, nil
}

func (r *VisibleModelResolver) resolve(ctx context.Context, groupID *int64, platform string, metadata map[string][]UpstreamModelMetadata, mixedOnly ...bool) (VisibleModelSet, error) {
	view := VisibleModelSet{FullyEnumerable: true, Bindings: make(map[string][]AccountModelBinding)}
	ctx, err := r.freeze(ctx)
	if err != nil {
		return VisibleModelSet{}, err
	}
	if r == nil || r.gateway == nil {
		return view, nil
	}
	view.Models, err = r.gateway.resolveVisibleModelIDs(ctx, groupID, platform, metadata, &view, mixedOnly...)
	if err != nil {
		return VisibleModelSet{}, err
	}
	if err := r.validate(ctx); err != nil {
		return VisibleModelSet{}, err
	}
	revision, _ := ctx.Value(visibleModelRevisionKey{}).(visibleModelRevision)
	view.Revision = fmt.Sprintf("%d:%s", revision.generation, ModelRegistrySnapshotFromContext(ctx).Version())
	return view, nil
}

// A mapping target is private unless explicitly also declared as a public key.
func internalMappedTarget(account *Account, model string) bool {
	mapping := configuredModelMapping(account)
	if _, public := mapping[model]; public {
		return false
	}
	for alias, target := range mapping {
		if alias != model && target == model {
			return true
		}
	}
	return false
}

func (s *GatewayService) resolveVisibleModelIDs(ctx context.Context, groupID *int64, platform string, metadata map[string][]UpstreamModelMetadata, view *VisibleModelSet, mixedOnly ...bool) ([]string, error) {
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
	privateTargets := make(map[string]bool)
	explicitPublicNames := make(map[string]bool)
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
			if !internalMappedTarget(account, member.ID) {
				candidates[member.ID] = struct{}{}
			}
		}
		for alias := range account.GetModelMapping() {
			if strings.Contains(alias, "*") {
				view.FullyEnumerable = false
			} else {
				candidates[alias] = struct{}{}
				explicitPublicNames[alias] = true
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
		for key, entries := range channels.wildcardMappingByGP {
			if key.groupID == *groupID && len(entries) > 0 && (platform == PlatformComposite || platform == "" || key.platform == platform) {
				view.FullyEnumerable = false
				for _, entry := range entries {
					if entry.target != "" {
						privateTargets[entry.target] = true
					}
				}
			}
		}
		for key, target := range channels.mappingByGroupModel {
			if key.groupID == *groupID && (platform == PlatformComposite || platform == "" || key.platform == platform) {
				candidates[key.model] = struct{}{}
				explicitPublicNames[key.model] = true
				if key.model != target {
					privateTargets[target] = true
				}
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
				if route.MatchType != CompositeRouteMatchExact {
					view.FullyEnumerable = false
					if route.UpstreamModel != "" {
						privateTargets[route.UpstreamModel] = true
					}
				}
				if route.MatchType == CompositeRouteMatchExact {
					candidates[route.PublicModel] = struct{}{}
					explicitPublicNames[route.PublicModel] = true
					if route.UpstreamModel != route.PublicModel && route.UpstreamModel != "" {
						privateTargets[route.UpstreamModel] = true
					}
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
		if strings.ContainsAny(model, "*?") || (privateTargets[model] && !explicitPublicNames[model]) {
			continue
		}
		if !visibleModelPolicyAllows(ctx, model) {
			continue
		}
		if group != nil && group.ModelAllowlistEnabled() && !group.ModelAllowlist.Allows(model) {
			continue
		}
		supported := false
		for _, endpoint := range []string{CompositeRouteEndpointResponses, CompositeRouteEndpointChatCompletions, CompositeRouteEndpointMessages, CompositeRouteEndpointGemini, CompositeRouteEndpointEmbeddings, CompositeRouteEndpointImages, string(OpenAIEndpointCapabilityLive), string(OpenAIEndpointCapabilityAlphaSearch), string(OpenAIEndpointCapabilityGrokMediaGeneration)} {
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
					view.Bindings[model] = append(view.Bindings[model], binding)
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

// visibleModelsForGroups evaluates all panel groups under one registry and
// authoritative catalog generation; a concurrent edit fails the whole view.
func visibleModelsForGroups(ctx context.Context, resolver *VisibleModelResolver, groups []Group) (context.Context, map[int64]map[string]bool, error) {
	frozen, err := resolver.freeze(ctx)
	if err != nil {
		return ctx, nil, err
	}
	visible := make(map[int64]map[string]bool)
	if resolver == nil {
		return frozen, visible, nil
	}
	for _, group := range groups {
		view, err := resolver.Resolve(frozen, &group.ID, group.Platform)
		if err != nil {
			return ctx, nil, err
		}
		members := make(map[string]bool, len(view.Models))
		for _, model := range view.Models {
			members[model] = true
		}
		visible[group.ID] = members
	}
	return frozen, visible, nil
}

func panelModelVisible(ctx context.Context, group *Group, model string, visible map[int64]map[string]bool) bool {
	if group == nil || !visibleModelPolicyAllows(ctx, model) || !group.ModelAllowlist.Allows(model) {
		return false
	}
	if members, resolved := visible[group.ID]; resolved {
		return members[model]
	}
	return true // legacy test adapters without a catalog repository
}
