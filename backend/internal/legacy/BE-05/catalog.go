package legacy

import (
	"context"
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
	store    *postgres.Store
	plaza    *service.ModelPlazaService
	settings *service.SettingService
	keys     *service.APIKeyService
	ops      *service.OpsService
}

func NewSource(store *postgres.Store, plaza *service.ModelPlazaService, settings *service.SettingService, keys *service.APIKeyService, ops *service.OpsService) *Source {
	return &Source{store, plaza, settings, keys, ops}
}

func (s *Source) Load(ctx context.Context, p catalog.Perspective, window catalog.Window, now time.Time) (catalog.Input, error) {
	runtime := s.settings.GetModelPlazaRuntime(ctx)
	if !runtime.Enabled {
		return catalog.Input{}, fault.NotFound
	}
	if p.Public() && runtime.RequireAuth {
		return catalog.Input{}, fault.Unauthenticated
	}
	ctx = domain.WithWorkspaceScope(ctx, p.Workspace)
	snapshot := service.ModelRegistrySnapshotFromContext(ctx)
	ctx = service.WithModelRegistrySnapshot(ctx, snapshot)
	var groups []service.PlazaGroup
	var err error
	allowedGroups := map[int64]bool{}
	var userRates map[int64]float64
	var priceErr error
	endpoints := map[int64]postgres.EndpointEvidence{}
	if p.Public() {
		groups, err = s.plaza.ListVisibleGroups(ctx, nil, false)
	} else {
		ctx = scope.With(ctx, scope.Authorized{WorkspaceID: p.Workspace, ProjectID: p.Project, ActorID: p.Actor, Version: p.AccessVersion})
		groups, allowedGroups, err = s.scopedGroups(ctx, p)
		if err == nil {
			endpoints, err = s.store.EndpointEvidence(ctx, p, endpointAccounts(groups))
			userRates, priceErr = s.keys.GetUserGroupRates(ctx, p.Actor)
		}
	}
	if err != nil {
		return catalog.Input{}, err
	}

	out := catalog.Input{RegistryVersion: snapshot.Version(), Registry: snapshot.CatalogFacts(now), Metrics: make(map[string]catalog.Metrics)}
	if err := s.store.ImportRegistry(ctx, out.RegistryVersion, out.Registry); err != nil {
		return catalog.Input{}, err
	}
	modelAt := make(map[string]int)
	metricIDs := map[postgres.MetricKey]string{}
	for _, group := range groups {
		for _, model := range group.Models {
			id := catalog.StableID(model.Platform, model.Name)
			i, exists := modelAt[id]
			if !exists {
				i = len(out.Models)
				modelAt[id] = i
				out.Models = append(out.Models, catalog.Candidate{Provider: model.Platform, Model: model.Name})
			}
			offer := catalog.Offer{GroupID: group.ID, Access: allowedGroups[group.ID], Eligible: len(model.CatalogBindings) > 0}
			if p.Public() || offer.Access {
				if priceErr == nil {
					offer.Price = priceFor(model, group, userRates, p.Public())
				} else {
					offer.Price.State = "error"
				}
			}
			for _, binding := range model.CatalogBindings {
				e := endpoints[binding.AccountID]
				offer.Facts = append(offer.Facts, binding.CatalogModelFacts(discoveryExpiry(binding, e), now)...)
				if endpoint, ok := projectEndpoint(p, model.Platform, binding, e, now); ok {
					if offer.Access && p.EndpointMetadata {
						for i := range endpoint.Capabilities {
							capability := &endpoint.Capabilities[i]
							capability.VerifiedExtensionActions = verifiedExtensionActions(*capability, e.VerifiedActions[model.Name])
						}
					}
					offer.Endpoints = append(offer.Endpoints, endpoint)
					offer.Capabilities = append(offer.Capabilities, endpoint.Capabilities...)
				}
			}
			out.Models[i].Offers = append(out.Models[i].Offers, offer)
			if offer.Access {
				key := postgres.MetricKey{Group: group.ID, Model: model.Name}
				if prior, have := metricIDs[key]; have && prior != id {
					metricIDs[key] = ""
				} else {
					metricIDs[key] = id
				}
			}
		}
	}
	if !p.Public() && len(metricIDs) > 0 {
		out.Metrics, err = s.store.Metrics(ctx, p, window, metricIDs, s.ops.IsMonitoringEnabled(ctx))
		if err != nil && ctx.Err() != nil {
			return catalog.Input{}, err
		}
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

func (s *Source) scopedGroups(ctx context.Context, p catalog.Perspective) ([]service.PlazaGroup, map[int64]bool, error) {
	resources, err := s.store.ProjectResources(ctx, p)
	if err != nil {
		return nil, nil, err
	}
	selection := service.PlazaCatalogSelection{Groups: map[int64]bool{}, Channels: map[int64]bool{}, Accounts: map[int64]domain.ActionIdentity{}}
	for _, resource := range resources {
		switch resource.Kind {
		case "groups":
			selection.Groups[resource.ID] = true
		case "channels":
			selection.Channels[resource.ID] = true
		case "accounts":
			selection.Accounts[resource.ID] = domain.ActionIdentity{Platform: resource.Platform, HostedAccountType: resource.AccountType}
		}
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
	if err != nil {
		return nil, nil, err
	}
	current, err := s.store.ProjectResources(ctx, p)
	if err == nil && !slices.Equal(resources, current) {
		err = service.ErrVisibleModelRevisionChanged
	}
	return groups, access, err
}

func endpointAccounts(groups []service.PlazaGroup) []int64 {
	seen := map[int64]struct{}{}
	for _, group := range groups {
		for _, model := range group.Models {
			for _, binding := range model.CatalogBindings {
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
