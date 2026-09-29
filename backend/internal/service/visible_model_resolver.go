package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
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
	// ExecutableEndpoints contains the complete, deduplicated endpoint union.
	ExecutableEndpoints map[string][]string `json:"-"`
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
	snapshot, _ := ctx.Value(modelAccessSnapshotKey{}).(ModelAccessSnapshot)
	if !snapshot.KeyGroup.Allows(model) {
		return false
	}
	for _, excluded := range snapshot.KeyExclude {
		if excluded == model {
			return false
		}
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
	generation      int64
	authoritative   bool
	channels        *ChannelService
	channelSnapshot *channelCache
}

func (r *VisibleModelResolver) freeze(ctx context.Context) (context.Context, error) {
	return r.freezeMode(ctx, false)
}

// freezeMode 在 replace=true 时强制重新冻结目录版本（忽略已存在的 revision），
// 供目录版本竞态后的有限次重试使用；快照也一并重读，避免用旧快照对比新版本。
func (r *VisibleModelResolver) freezeMode(ctx context.Context, replace bool) (context.Context, error) {
	if r == nil || r.gateway == nil {
		return ctx, nil
	}
	if _, ok := r.gateway.accountRepo.(ModelCatalogAccountRepository); !ok {
		return ctx, nil
	}
	if !replace {
		if _, ok := ctx.Value(visibleModelRevisionKey{}).(visibleModelRevision); ok {
			return ctx, nil
		}
	}
	generation, authoritative, err := modelCatalogGeneration(ctx, r.gateway.accountRepo, "catalog")
	if err != nil {
		return ctx, err
	}
	if replace {
		// Authenticated policies cannot be refreshed as keyID=0: that would
		// discard KeyGroup/KeyExclude restrictions during a revision race.
		// Let the caller retry the complete authentication/read transaction.
		if _, attached := ctx.Value(visibleModelPolicyKey{}).([]string); attached {
			return ctx, ErrVisibleModelRevisionChanged
		}
		if reader, ok := r.gateway.accountRepo.(ModelAccessPolicyReader); ok {
			snapshot, readErr := reader.ReadModelAccessSnapshot(ctx, 0)
			if readErr != nil {
				return ctx, readErr
			}
			ctx = context.WithValue(ctx, modelAccessSnapshotKey{}, snapshot)
		}
	} else if _, loaded := ctx.Value(modelAccessSnapshotKey{}).(ModelAccessSnapshot); !loaded {
		if reader, ok := r.gateway.accountRepo.(ModelAccessPolicyReader); ok {
			snapshot, readErr := reader.ReadModelAccessSnapshot(ctx, 0)
			if readErr != nil {
				return ctx, readErr
			}
			ctx = context.WithValue(ctx, modelAccessSnapshotKey{}, snapshot)
		}
	}
	ctx = WithModelRegistrySnapshot(ctx, ModelRegistrySnapshotFromContext(ctx))
	if snapshot, ok := ctx.Value(modelAccessSnapshotKey{}).(ModelAccessSnapshot); ok && snapshot.RevisionKnown && snapshot.Generation != generation {
		return ctx, ErrVisibleModelRevisionChanged
	}
	revision := visibleModelRevision{generation: generation, authoritative: authoritative}
	if channels := r.gateway.channelService; channels != nil {
		// Reuse only immutable channel evidence within this projection. The
		// generation is still read before and after the complete response, and
		// user/Key authorization is never shared between requests. Clear an old
		// projection revision when retrying so its channel snapshot is not reused.
		readContext := context.WithValue(ctx, visibleModelRevisionKey{}, visibleModelRevision{})
		snapshot, readErr := channels.loadCache(readContext)
		if readErr != nil {
			return ctx, readErr
		}
		revision.channels, revision.channelSnapshot = channels, snapshot
	}
	return context.WithValue(ctx, visibleModelRevisionKey{}, revision), nil
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
// For each public name, metadata and Bindings have matching indexes. Action
// serializers must pair each specification with that binding's endpoint before
// combining SupportedMethods across exits (notably Gemini countTokens).
func (s *GatewayService) VisibleModelsWithMetadata(ctx context.Context, groupID *int64, platform string) (VisibleModelSet, map[string][]UpstreamModelMetadata, error) {
	metadata := make(map[string][]UpstreamModelMetadata)
	view, err := (&VisibleModelResolver{gateway: s}).resolve(ctx, groupID, platform, metadata)
	if err != nil {
		return VisibleModelSet{}, nil, err
	}
	return view, metadata, nil
}

// visibleModelResolveMaxAttempts bounds retries when the visible model catalog
// revision moves mid-resolution (e.g. a concurrent upstream catalog refresh
// publishes a new generation). Only the resolver that froze the revision itself
// retries; nested callers under an outer freeze keep fail-fast consistency.
const visibleModelResolveMaxAttempts = 3

func (r *VisibleModelResolver) resolve(ctx context.Context, groupID *int64, platform string, metadata map[string][]UpstreamModelMetadata, mixedOnly ...bool) (VisibleModelSet, error) {
	view := VisibleModelSet{FullyEnumerable: true, Bindings: make(map[string][]AccountModelBinding), ExecutableEndpoints: make(map[string][]string)}
	_, preFrozen := ctx.Value(visibleModelRevisionKey{}).(visibleModelRevision)
	ctx, err := r.freeze(ctx)
	if err != nil {
		return VisibleModelSet{}, err
	}
	if r == nil || r.gateway == nil {
		return view, nil
	}
	for attempt := 1; ; attempt++ {
		view.Models, err = r.gateway.resolveVisibleModelIDs(ctx, groupID, platform, metadata, &view, mixedOnly...)
		if err != nil && !errors.Is(err, ErrVisibleModelRevisionChanged) {
			return VisibleModelSet{}, err
		}
		validateErr := err
		if validateErr == nil {
			validateErr = r.validate(ctx)
		}
		if validateErr == nil {
			break
		}
		if preFrozen || attempt >= visibleModelResolveMaxAttempts || !errors.Is(validateErr, ErrVisibleModelRevisionChanged) {
			return VisibleModelSet{}, validateErr
		}
		ctx, err = r.freezeMode(ctx, true)
		if err != nil {
			return VisibleModelSet{}, err
		}
		// 上一次尝试可能已部分填充 Bindings/ExecutableEndpoints/metadata，重算前清空。
		view.Bindings = make(map[string][]AccountModelBinding)
		view.ExecutableEndpoints = make(map[string][]string)
		view.FullyEnumerable = true
		for key := range metadata {
			delete(metadata, key)
		}
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
		platforms = domain.ConcretePlatforms()
	} else if !domain.IsConcretePlatform(platform) {
		return []string{}, nil
	}
	var accounts []Account
	var err error
	batch, batched := ctx.Value(catalogGroupSnapshotKey{}).(*catalogGroupSnapshot)
	if batched && groupID != nil {
		accounts = append([]Account(nil), batch.accounts[*groupID]...)
	} else {
		includeGrouped := s.cfg != nil && s.cfg.RunMode == "simple"
		accounts, err = s.catalogReadAccounts(ctx, fmt.Sprintf("group:%v:platforms:%v:simple:%t", catalogGroupID(groupID), platforms, includeGrouped), func() ([]Account, error) {
			return reader.ListModelCatalogAccounts(ctx, groupID, platforms, includeGrouped)
		})
		if err != nil {
			return []string{}, err
		}
	}
	candidates := make(map[string]struct{})
	privateTargets := make(map[string]bool)
	explicitPublicNames := make(map[string]bool)
	for i := range accounts {
		account := &accounts[i]
		if len(mixedOnly) > 0 && mixedOnly[0] && account.Platform == PlatformAntigravity && !account.IsMixedSchedulingEnabled() {
			continue
		}
		catalog := account.EffectiveUpstreamModelCatalogForCredentialAccount(account.modelCatalogCredentialAccount)
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
	if batched && groupID != nil {
		group = batch.groups[*groupID]
	} else if groupID != nil && s.groupRepo != nil {
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
		for _, endpoint := range []string{CompositeRouteEndpointResponses, CompositeRouteEndpointChatCompletions, CompositeRouteEndpointMessages, CompositeRouteEndpointCountTokens, CompositeRouteEndpointGemini, CompositeRouteEndpointEmbeddings, CompositeRouteEndpointImages, string(OpenAIEndpointCapabilityLive), string(OpenAIEndpointCapabilityAlphaSearch), string(OpenAIEndpointCapabilityGrokMediaGeneration)} {
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
					view.ExecutableEndpoints[model] = append(view.ExecutableEndpoints[model], endpoint)

				}
			}
		}
		if supported {
			view.ExecutableEndpoints[model] = dedupeAndSortModelIDs(view.ExecutableEndpoints[model])
			if metadata != nil {
				bindings := view.Bindings[model]
				projections := make([]UpstreamModelMetadata, len(bindings))
				for index, binding := range bindings {
					projection := UpstreamModelMetadata{}
					if snapshot := specifications[binding.AccountID]; snapshot != nil {
						candidate := snapshot.Models[binding.UpstreamModel]
						if candidate.ProviderProfile != "" && candidate.ProviderProfile == profiles[binding.AccountID] {
							projection = candidate
						}
					}
					projections[index] = visibleModelMetadata(ModelRegistrySnapshotFromContext(ctx), profiles[binding.AccountID], binding.UpstreamModel, projection)
				}
				metadata[model] = projections
			}
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
	frozen, err = resolver.preloadCatalogGroups(frozen, groups)
	if err != nil {
		return ctx, nil, err
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

func catalogGroupID(groupID *int64) string {
	if groupID == nil {
		return "ungrouped"
	}
	return fmt.Sprint(*groupID)
}
