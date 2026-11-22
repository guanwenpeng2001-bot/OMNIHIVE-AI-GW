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
	registry := ModelRegistrySnapshotFromContext(ctx).CatalogFacts(time.Now())
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
			for _, model := range PlazaVisibleModels(&channel, group.ID) {
				configure(model.Platform)
				if entry := add(model.Platform, model.Name); entry != nil {
					entry.Published = true
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
			if snapshot := account.EffectiveUpstreamModelCatalogForCredentialAccount(account.modelCatalogCredentialAccount); snapshot != nil {
				for _, model := range snapshot.DiscoveredModels {
					if !internalMappedTarget(account, model) {
						if entry := add(platform, model); entry != nil && entry.CatalogOnly {
							if metadata, ok := account.GetUpstreamModelMetadata(model); ok {
								entry.CatalogObservations = append(entry.CatalogObservations, PlazaCatalogBinding{
									AccountModelBinding: AccountModelBinding{AccountID: account.ID, UpstreamModel: model, EvidenceObservedAt: snapshot.ObservedAt},
									Identity:            domain.ActionIdentity{Platform: account.Platform, HostedAccountType: account.Type}, Metadata: metadata,
								})
							}
						}
					}
				}
			}
		}
		for _, entry := range registry {
			if !platforms[entry.Provider] {
				continue
			}
			// The verified snapshot retains each reviewed origin. Community
			// references and private account observations cannot publish inventory.
			switch entry.Facts.Source {
			case "approved_registry", catalog.SourceBuiltin, catalog.SourcePreset:
				add(entry.Provider, entry.Model)
			}
		}
	}
	return nil
}
