package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
)

type catalogRegistryFactsKey struct{}

func WithCatalogRegistryFacts(ctx context.Context, facts []catalog.RegistryModel) context.Context {
	return context.WithValue(ctx, catalogRegistryFactsKey{}, facts)
}

func plazaOffer(channel *Channel, model SupportedModel, bindings []PlazaCatalogBinding, eligible bool) PlazaCatalogOffer {
	basis := "explicit"
	if _, ok := channel.FeaturesConfig[PlazaConfirmationConfigKey]; !ok {
		basis = "legacy_default"
	}
	mapped := model.Name
	if target := channel.ModelMapping[model.Platform][model.Name]; target != "" {
		mapped = target
	}
	return PlazaCatalogOffer{ChannelID: channel.ID, BillingModelSource: channel.BillingModelSource,
		ChannelMappedModel: mapped, PublicationBasis: basis, Bindings: bindings, Eligible: eligible}
}

// ListPublicCatalogGroups adds configured-platform inventory to the live plaza
// offers. The v1 group/pricing view deliberately retains ListVisibleGroups.
func (s *ModelPlazaService) ListPublicCatalogGroups(ctx context.Context) ([]PlazaGroup, error) {
	return s.listGroups(ctx, plazaGroupVisibility(nil, false), nil, true)
}

type configuredCatalogAccountRepository interface {
	ListConfiguredModelCatalogAccountsForGroups(context.Context, []int64) ([]Account, error)
}

func configuredCatalogAccounts(ctx context.Context, resolver *VisibleModelResolver, groups []Group) ([]Account, error) {
	if resolver == nil || resolver.gateway == nil || len(groups) == 0 {
		return nil, nil
	}
	ids := make([]int64, 0, len(groups))
	for _, group := range groups {
		ids = append(ids, group.ID)
	}
	slices.Sort(ids)
	repository := resolver.gateway.accountRepo
	if reader, ok := repository.(configuredCatalogAccountRepository); ok {
		return resolver.gateway.catalogReadAccounts(ctx, fmt.Sprintf("configured-groups:%v", ids), func() ([]Account, error) {
			return reader.ListConfiguredModelCatalogAccountsForGroups(ctx, ids)
		})
	}
	// Older adapters retain their existing bounded group reads.
	if reader, ok := repository.(ModelCatalogAccountRepository); ok {
		var result []Account
		for _, group := range groups {
			accounts, err := reader.ListModelCatalogAccounts(ctx, &group.ID, domain.ConcretePlatforms(), false)
			if err != nil {
				return nil, err
			}
			for _, account := range accounts {
				account.GroupIDs = []int64{group.ID}
				result = append(result, account)
			}
		}
		return result, nil
	}
	return nil, nil
}

func (s *ModelPlazaService) expandConfiguredCatalog(ctx context.Context, groups []Group, channels []Channel, output map[int64]*PlazaGroup, selection *PlazaCatalogSelection, resolver *VisibleModelResolver) error {
	accounts, err := configuredCatalogAccounts(ctx, resolver, groups)
	if err != nil {
		return err
	}
	registry, ok := ctx.Value(catalogRegistryFactsKey{}).([]catalog.RegistryModel)
	if !ok {
		registry = ModelRegistrySnapshotFromContext(ctx).CatalogFacts(time.Now())
	}
	now := catalogReadTime(ctx)
	for _, group := range groups {
		pg := output[group.ID]
		platforms := map[string]bool{}
		accept := func(platform string) bool {
			return domain.IsConcretePlatform(platform) && (group.Platform == PlatformComposite ||
				domain.CanonicalPlatform(platform) == domain.CanonicalPlatform(group.Platform))
		}
		configure := func(platform string) {
			if accept(platform) {
				platforms[platform] = true
			}
		}
		configure(group.Platform)
		seen := map[PlazaModelIdentity]int{}
		for i, model := range pg.Models {
			seen[PlazaModelIdentity{Platform: model.Platform, Name: model.Name}] = i
		}
		add := func(platform, model string) *PlazaModel {
			identity := PlazaModelIdentity{Platform: platform, Name: model}
			if !accept(platform) || model == "" || strings.TrimSpace(model) != model || strings.ContainsAny(model, "*?") {
				return nil
			}
			if selection != nil && selection.SelectedKey && !visibleModelPolicyAllows(ctx, model) {
				return nil
			}
			if i, exists := seen[identity]; exists {
				return &pg.Models[i]
			}
			seen[identity] = len(pg.Models)
			pg.Models = append(pg.Models, PlazaModel{Name: model, Platform: platform, CatalogOnly: true})
			return &pg.Models[len(pg.Models)-1]
		}
		for _, channel := range channels {
			if channel.Status != StatusActive || !slices.Contains(channel.GroupIDs, group.ID) || (selection != nil && !selection.Channels[channel.ID]) {
				continue
			}
			for platform := range channel.ModelMapping {
				configure(platform)
			}
			for _, price := range channel.ModelPricing {
				configure(price.Platform)
			}
			// An operator-published name remains listed during a quota outage.
			channel.normalizeBillingModelSource()
			for _, model := range PlazaVisibleModels(&channel, group.ID) {
				configure(model.Platform)
				if entry := add(model.Platform, model.Name); entry != nil && !slices.ContainsFunc(entry.CatalogOffers, func(o PlazaCatalogOffer) bool { return o.ChannelID == channel.ID }) {
					entry.CatalogOffers = append(entry.CatalogOffers, plazaOffer(&channel, model, nil, false))
				}
			}
		}
		for i := range accounts {
			account := &accounts[i]
			if !account.IsActive() || !slices.Contains(account.GroupIDs, group.ID) ||
				(group.RequiredAccountType() != "" && account.Type != group.RequiredAccountType()) ||
				(group.Platform != PlatformComposite && !account.MatchesPlatform(group.Platform, group.RequiredAccountType())) {
				continue
			}
			if selection != nil {
				if _, ok := selection.Accounts[account.ID]; !ok {
					continue
				}
				if account.IsShadow() {
					if _, ok := selection.Accounts[*account.ParentAccountID]; !ok {
						continue
					}
				}
			}
			platform := account.Platform
			if group.Platform != PlatformComposite {
				platform = group.Platform // preserve hosted group identity
			}
			configure(platform)
			// Unpublished account IDs remain private. Project owners can see
			// discovered inventory, but mapped upstream targets stay hidden.
			if selection == nil {
				continue
			}
			appendCatalogDiscoveries(account, platform, selection.IncludeHistorical, now, add)
		}
		for _, entry := range registry {
			if platforms[entry.Provider] {
				add(entry.Provider, entry.Model)
			}
		}
	}
	return nil
}

func appendCatalogDiscoveries(account *Account, platform string, includeHistorical bool, now time.Time, add func(string, string) *PlazaModel) {
	current := account.EffectiveUpstreamModelCatalogForCredentialAccount(account.modelCatalogCredentialAccount)
	snapshot := current
	if snapshot == nil && includeHistorical {
		snapshot = account.GetUpstreamModelCatalog()
	}
	if snapshot == nil || snapshot.SourceKind == "platform-candidates" || snapshot.SourceKind == modelEvidenceLegacyMappingUpgrade {
		return
	}
	observed, _ := time.Parse(time.RFC3339Nano, snapshot.ObservedAt)
	state := "current"
	if current == nil {
		state = "invalidated"
	} else if observed.IsZero() || observed.After(now) || !now.Before(observed.Add(24*time.Hour)) {
		state = "stale"
	}
	for _, model := range snapshot.DiscoveredModels {
		if internalMappedTarget(account, model) {
			continue
		}
		if entry := add(platform, model); entry != nil {
			entry.Discovered = entry.Discovered || state == "current"
			metadata, _ := account.GetUpstreamModelMetadata(model)
			entry.CatalogObservations = append(entry.CatalogObservations, PlazaCatalogBinding{
				AccountModelBinding: AccountModelBinding{AccountID: account.ID, UpstreamModel: model, EvidenceObservedAt: snapshot.ObservedAt,
					SnapshotRevision: snapshot.Revision, IdentityRevision: snapshot.IdentityRevision, RulesRevision: snapshot.RulesRevision,
					CredentialAccountID: snapshot.CredentialAccountID, CredentialIdentityRevision: snapshot.CredentialIdentityRevision},
				Identity: domain.ActionIdentity{Platform: account.Platform, HostedAccountType: account.Type}, Metadata: metadata,
				DiscoveryState: state, AccountLabel: account.Name,
			})
		}
	}
}

// AdminCatalogInventory adds discovery and approved candidates from accounts
// with no active group. IDs are authorized by the caller's workspace selection.
func (s *ModelPlazaService) AdminCatalogInventory(ctx context.Context, ids []int64, registry []catalog.RegistryModel) ([]PlazaModel, error) {
	resolver := s.catalogResolver()
	if resolver == nil || resolver.gateway == nil || len(ids) == 0 {
		return nil, nil
	}
	accounts, err := resolver.gateway.accountRepo.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]*Account, len(accounts))
	for _, account := range accounts {
		if account != nil {
			byID[account.ID] = account
		}
	}
	var out []PlazaModel
	index := map[catalog.ModelIdentity]int{}
	add := func(provider, model string) *PlazaModel {
		id := catalog.NewModelIdentity(provider, model)
		if !domain.IsConcretePlatform(id.Provider) || model == "" || strings.TrimSpace(model) != model || strings.ContainsAny(model, "*?") {
			return nil
		}
		i, ok := index[id]
		if !ok {
			i = len(out)
			index[id] = i
			out = append(out, PlazaModel{Name: id.Model, Platform: id.Provider, CatalogOnly: true})
		}
		return &out[i]
	}
	platforms := map[string]bool{}
	for _, id := range ids {
		a := byID[id]
		if a == nil {
			continue
		}
		if a.IsShadow() && byID[*a.ParentAccountID] == nil {
			continue
		}
		account := *a
		if account.IsShadow() {
			account.modelCatalogCredentialAccount = byID[*account.ParentAccountID]
		}
		platforms[account.Platform] = true
		appendCatalogDiscoveries(&account, account.Platform, true, catalogReadTime(ctx), add)
	}
	for _, entry := range registry {
		if platforms[entry.Provider] {
			add(entry.Provider, entry.Model)
		}
	}
	return out, nil
}
