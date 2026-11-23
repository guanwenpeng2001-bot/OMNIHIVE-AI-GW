package legacy

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog/postgres"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/pkg/pluginapi/v2/actions"
)

type Source struct {
	store          *postgres.Store
	plaza          *service.ModelPlazaService
	settings       *service.SettingService
	keys           *service.APIKeyService
	ops            *service.OpsService
	prices         catalog.PricePort
	keysForCatalog KeyCatalogPort
}

func NewSource(store *postgres.Store, plaza *service.ModelPlazaService, settings *service.SettingService, keys *service.APIKeyService, ops *service.OpsService) *Source {
	return NewSourceWithPorts(store, plaza, settings, keys, ops, nil, nil)
}

// KeyCatalogPort authorizes the selected Key and attaches its model policy
// before reading model, endpoint, or tariff evidence.
type KeyCatalogPort interface {
	AuthorizeCatalogKey(context.Context, catalog.Perspective) (context.Context, int64, error)
}

func NewSourceWithPorts(store *postgres.Store, plaza *service.ModelPlazaService, settings *service.SettingService, keys *service.APIKeyService, ops *service.OpsService, prices catalog.PricePort, keyCatalog KeyCatalogPort) *Source {
	return &Source{store: store, plaza: plaza, settings: settings, keys: keys, ops: ops, prices: prices, keysForCatalog: keyCatalog}
}

func (s *Source) scopedGroups(ctx context.Context, p catalog.Perspective, resources []postgres.Resource, keyGroup int64) ([]service.PlazaGroup, map[int64]bool, error) {
	selection := service.PlazaCatalogSelection{Groups: map[int64]bool{}, Channels: map[int64]bool{}, Accounts: map[int64]domain.ActionIdentity{}, SelectedKey: keyGroup > 0}
	for _, resource := range resources {
		switch resource.Kind {
		case "groups":
			if keyGroup == 0 || keyGroup == resource.ID {
				selection.Groups[resource.ID] = true
			}
		case "channels":
			selection.Channels[resource.ID] = true
		case "accounts":
			selection.Accounts[resource.ID] = domain.ActionIdentity{Platform: resource.Platform, HostedAccountType: resource.AccountType}
		}
	}
	if p.Kind == catalog.PerspectiveAdmin {
		allowed := make(map[int64]struct{}, len(selection.Groups))
		for group := range selection.Groups {
			allowed[group] = struct{}{}
		}
		selection.IncludeHistorical = true
		groups, err := s.plaza.ListProjectCatalogGroups(ctx, allowed, true, selection)
		return groups, selection.Groups, err
	}
	if keyGroup > 0 && !selection.Groups[keyGroup] {
		return nil, nil, fault.NotFound
	}
	allowed, restrict, err := s.keys.GetUserGroupVisibility(ctx, p.Actor)
	if err != nil {
		return nil, nil, err
	}
	bindable, err := s.keys.GetAvailableGroups(ctx, p.Actor)
	if err != nil {
		return nil, nil, err
	}
	access := make(map[int64]bool, len(bindable))
	for _, group := range bindable {
		access[group.ID] = selection.Groups[group.ID]
	}
	groups, err := s.plaza.ListProjectCatalogGroups(ctx, allowed, restrict, selection)
	return groups, access, err
}

var catalogChanged = &fault.Fault{Status: 503, Reason: "CATALOG_CHANGED", Message: "The model catalog changed during the read. Try again."}

func (s *Source) Load(ctx context.Context, p catalog.Perspective, window catalog.Window, now time.Time) (catalog.Input, error) {
	for range 2 {
		out, err := s.load(ctx, p, window, now)
		if !errors.Is(err, service.ErrVisibleModelRevisionChanged) {
			return out, err
		}
	}
	return catalog.Input{}, catalogChanged
}

func (s *Source) load(ctx context.Context, p catalog.Perspective, window catalog.Window, now time.Time) (catalog.Input, error) {
	admin := p.Kind == catalog.PerspectiveAdmin
	var runtime service.ModelPlazaRuntime
	if p.Workspace <= 0 || !p.Public() && !admin && p.Project <= 0 {
		return catalog.Input{}, fault.NotFound
	}
	if !admin {
		runtime = s.settings.GetModelPlazaRuntime(ctx)
		if !runtime.Enabled {
			return catalog.Input{}, fault.NotFound
		}
		if p.Public() && runtime.RequireAuth && !catalog.PublicViewerAuthenticated(ctx) {
			return catalog.Input{}, fault.Unauthenticated
		}
	}
	ctx = domain.WithWorkspaceScope(ctx, p.Workspace)
	ctx = service.WithCatalogReadTime(ctx, now)
	snapshot := service.ModelRegistrySnapshotFromContext(ctx)
	ctx = service.WithModelRegistrySnapshot(ctx, snapshot)
	registry := snapshot.CatalogFacts(now)
	ctx = service.WithCatalogRegistryFacts(ctx, registry)
	if !p.Public() {
		ctx = scope.With(ctx, scope.Authorized{WorkspaceID: p.Workspace, ProjectID: p.Project, ActorID: p.Actor, Version: p.AccessVersion})
	}
	var err error
	var keyGroup int64
	if p.KeyID != 0 || p.Kind == catalog.PerspectiveSelectedKey {
		if s.keysForCatalog == nil || p.Public() || admin || p.KeyID <= 0 {
			return catalog.Input{}, fault.NotFound
		}
		ctx, keyGroup, err = s.keysForCatalog.AuthorizeCatalogKey(ctx, p)
		if err != nil {
			return catalog.Input{}, err
		}
		if keyGroup <= 0 {
			return catalog.Input{}, fault.NotFound
		}
	}
	fallbackRevision := s.plaza.CatalogPriceRevision()
	ctx, err = s.plaza.FreezeCatalogRead(ctx)
	if err != nil {
		return catalog.Input{}, err
	}
	priceRevision := fallbackRevision
	priceFence, hasPriceFence := s.prices.(catalog.PriceRevisionPort)
	if hasPriceFence {
		priceRevision, err = priceFence.PriceRevision(ctx)
		if err != nil {
			return catalog.Input{}, err
		}
	}
	var groups []service.PlazaGroup
	var resources []postgres.Resource
	allowedGroups := map[int64]bool{}
	var userRates map[int64]float64
	var priceErr error
	endpoints := map[int64]postgres.EndpointEvidence{}
	if p.Public() {
		groups, err = s.plaza.ListPublicCatalogGroups(ctx)
	} else {
		resources, err = s.store.ProjectResources(ctx, p)
		if err == nil {
			groups, allowedGroups, err = s.scopedGroups(ctx, p, resources, keyGroup)
		}
		if err == nil && admin {
			var accounts []int64
			for _, resource := range resources {
				if resource.Kind == "accounts" {
					accounts = append(accounts, resource.ID)
				}
			}
			var models []service.PlazaModel
			models, err = s.plaza.AdminCatalogInventory(ctx, accounts, registry)
			groups = append(groups, service.PlazaGroup{Models: models})
		}
		if err == nil && p.EndpointMetadata {
			endpoints, err = s.store.EndpointEvidence(ctx, p, endpointAccounts(groups))
		}
		if err == nil && !admin {
			userRates, priceErr = s.keys.GetUserGroupRates(ctx, p.Actor)
		}
	}
	if err != nil {
		return catalog.Input{}, err
	}

	out := catalog.Input{RegistryVersion: snapshot.Version(), Registry: registry, Metrics: make(map[string]catalog.Metrics)}
	if p.Public() {
		if err := s.store.ImportRegistry(ctx, out.RegistryVersion, out.Registry); err != nil {
			return catalog.Input{}, err
		}
	}
	generation, _ := service.CatalogReadGeneration(ctx)
	out.Revision = catalog.Digest(struct {
		Generation int64
		Price      string
		Resources  []postgres.Resource
	}{generation, priceRevision, resources})
	modelAt := make(map[string]int)
	metricIDs := map[postgres.MetricKey]string{}
	for _, group := range groups {
		for _, model := range group.Models {
			identity := catalog.NewModelIdentity(model.Platform, model.Name)
			id := identity.ID()
			i, exists := modelAt[id]
			if !exists {
				i = len(out.Models)
				modelAt[id] = i
				out.Models = append(out.Models, catalog.Candidate{Provider: identity.Provider, Model: identity.Model})
			}
			offers := s.modelOffers(ctx, p, model, group, allowedGroups[group.ID], userRates, priceErr, endpoints, now)
			out.Models[i].Offers = append(out.Models[i].Offers, offers...)
			for _, offer := range model.CatalogOffers {
				if offer.ChannelMappedModel != "" && offer.ChannelMappedModel != model.Name {
					out.Models[i].Canonicals = append(out.Models[i].Canonicals, offer.ChannelMappedModel)
				}
			}
			if allowedGroups[group.ID] && !admin {
				key := postgres.MetricKey{Group: group.ID, Model: model.Name}
				if prior, have := metricIDs[key]; have && prior != id {
					metricIDs[key] = ""
				} else {
					metricIDs[key] = id
				}
			}
		}
	}
	visibleEntries := make([]string, 0, len(out.Models))
	for _, model := range out.Models {
		visibleEntries = append(visibleEntries, catalog.StableID(model.Provider, model.Model))
	}
	out.Compatibility, err = s.store.Compatibility(ctx, visibleEntries)
	if err != nil {
		return catalog.Input{}, err
	}
	if !p.Public() && len(metricIDs) > 0 {
		out.Metrics, err = s.store.Metrics(ctx, p, window, metricIDs, s.ops != nil && s.ops.IsMonitoringEnabled(ctx))
		if err != nil && ctx.Err() != nil {
			return catalog.Input{}, err
		}
	}
	if err := s.plaza.ValidateCatalogRead(ctx); err != nil {
		return catalog.Input{}, err
	}
	if !p.Public() {
		current, err := s.store.ProjectResources(ctx, p)
		if err != nil {
			return catalog.Input{}, err
		}
		if !slices.Equal(resources, current) {
			return catalog.Input{}, service.ErrVisibleModelRevisionChanged
		}
	}
	if hasPriceFence {
		current, err := priceFence.PriceRevision(ctx)
		if err != nil {
			return catalog.Input{}, err
		}
		if priceRevision != current {
			return catalog.Input{}, service.ErrVisibleModelRevisionChanged
		}
	}
	if fallbackRevision != s.plaza.CatalogPriceRevision() {
		return catalog.Input{}, service.ErrVisibleModelRevisionChanged
	}
	if !admin && runtime != s.settings.GetModelPlazaRuntime(ctx) {
		return catalog.Input{}, service.ErrVisibleModelRevisionChanged
	}
	return out, nil
}
func verifiedExtensionActions(c catalog.Capability, labels []string) []string {
	if c.State != "fresh" || c.VerifiedAt == nil {
		return nil
	}
	var label string
	switch {
	case c.Protocol == "openai_responses" && c.Action == "text.chat":
		label = actions.ProviderResponses
	case c.Protocol == "openai_images" && c.Action == "image.generate":
		label = actions.TaskImageGenerate
	default:
		return nil
	}
	if slices.Contains(labels, label) {
		return []string{label}
	}
	return nil
}

func endpointAccounts(groups []service.PlazaGroup) []int64 {
	seen := map[int64]struct{}{}
	for _, group := range groups {
		for _, model := range group.Models {
			for _, binding := range append(slices.Clone(model.CatalogBindings), model.CatalogObservations...) {
				seen[binding.AccountID] = struct{}{}
			}
		}
	}
	out := make([]int64, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	return out
}

func discoveryExpiry(binding service.PlazaCatalogBinding, e postgres.EndpointEvidence) time.Time {
	observed, err := time.Parse(time.RFC3339Nano, binding.EvidenceObservedAt)
	if err != nil {
		return time.Time{}
	}
	expiry := observed.Add(24 * time.Hour)
	if e.LegacyResult == "failed" || e.LegacyResult == "credentials_invalid" {
		if failedAt, err := time.Parse(time.RFC3339Nano, e.LegacyAttempt); err == nil && failedAt.After(observed) && failedAt.Before(expiry) {
			expiry = failedAt
		}
	}
	return expiry
}

var awsRegion = regexp.MustCompile(`^(us|eu|ap|sa|ca|me|af|il|mx|cn)(-gov)?-[a-z]+-[0-9]+$`)

func projectEndpoint(p catalog.Perspective, provider string, b service.PlazaCatalogBinding, e postgres.EndpointEvidence, now time.Time) (catalog.Endpoint, bool) {
	action, protocol, adapterAction := catalog.GatewayAction(b.Endpoint)
	if adapterAction == "" || e.Provider == "" {
		return catalog.Endpoint{}, false
	}
	d := domain.PlatformAction(b.Identity, adapterAction)
	mode := "unknown"
	switch b.Endpoint {
	case service.CompositeRouteEndpointResponses:
		if descriptor, ok := domain.LookupPlatform(b.Identity.Platform); ok && descriptor.ResponsesPOST != "" && !descriptor.MultiProtocol {
			mode = string(descriptor.ResponsesPOST)
		}
	case service.CompositeRouteEndpointMessages:
		if b.Identity.Platform == service.PlatformAnthropic && domain.HostedAccountType(b.Identity.Platform) == "" &&
			b.Identity.HostedAccountType != domain.AccountTypeBedrock && b.Identity.HostedAccountType != domain.AccountTypeServiceAccount {
			mode = "native"
		}
	}
	tools := "unknown"
	if action == "text.chat" && service.AuthoritativeCatalogModelField(b.Metadata, "supports_function_calling") {
		if value := b.Metadata.SupportsFunctionCalling; value != nil && (!*value || mode == "native") {
			tools = catalog.Support(value)
		}
	}
	observed, _ := time.Parse(time.RFC3339Nano, b.EvidenceObservedAt)
	evidence := catalog.Observation("gateway_visibility", "unknown", observed, discoveryExpiry(b, e), now)
	var verified *time.Time
	if e.ConnectionActive && slices.Contains(e.VerifiedModels, b.UpstreamModel) {
		connection := catalog.Observation("connection_verification", "unknown", e.VerificationAt, e.VerificationExpiry, now)
		if d.Status == domain.ActionImplemented {
			verified = connection.ObservedAt
		}
		if e.VerificationFailed && connection.ObservedAt != nil {
			connection.State = "stale"
		}
		evidence = connection
	}
	capability := catalog.Capability{Action: action, Protocol: protocol, Mode: mode, Tools: tools, Stream: "unknown", VerifiedAt: verified,
		Evidence: evidence, Limitations: []string{}}
	// Connection verification checks authorization and discovery. The model
	// binding above supplies endpoint eligibility for the advertised action.
	if verified != nil {
		capability.Limitations = []string{"action_invocation_unmeasured"}
	}
	endpoint := catalog.Endpoint{
		ID: "ep_" + catalog.Digest([2]int64{p.Project, b.AccountID})[:32], Provider: provider, Regions: []string{},
		Training: "unknown", Retention: "unknown", Capabilities: []catalog.Capability{capability},
	}
	if e.PrivacyMode == service.PrivacyModeTrainingOff && e.Provider == service.PlatformOpenAI {
		endpoint.Training = "disabled"
		privacy := catalog.Observation("account_privacy_setting", "unknown", time.Time{}, time.Time{}, now)
		endpoint.PrivacyEvidence = &privacy
	}
	if b.Identity.HostedAccountType == domain.AccountTypeBedrock && !e.ForceGlobal && awsRegion.MatchString(e.Region) {
		endpoint.Regions = []string{e.Region}
		region := catalog.Observation("configured_endpoint_region", "unknown", time.Time{}, time.Time{}, now)
		endpoint.RegionEvidence = &region
	}
	return endpoint, true
}

func decimalText(n float64) string {
	return strconv.FormatFloat(n, 'f', -1, 64)
}
