package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

// ModelCatalogBatchAccountRepository keeps group membership on each account so
// shared accounts are decoded once, without broadening the requested group set.
type ModelCatalogBatchAccountRepository interface {
	ListModelCatalogAccountsForGroups(context.Context, []int64) ([]Account, error)
}

type catalogReadEvidence struct {
	implemented map[string][][]string
	discovered  map[string]bool
	catalog     *AccountModelCatalogSnapshot
	state       AccountModelCatalogState
	mapping     map[string]string
	members     map[string][]AccountCatalogModel
}

type catalogGroupSnapshotKey struct{}
type catalogGroupSnapshot struct {
	accounts map[int64][]Account
	groups   map[int64]*Group
}

// Only private, immutable evidence is shared. Key policy, subscription expiry,
// user prices, protocol decisions and endpoint eligibility are evaluated anew.
func prepareCatalogReadAccount(account *Account, parent *Account) {
	account.modelCatalogCredentialAccount = parent
	evidence := &catalogReadEvidence{
		catalog:     account.EffectiveUpstreamModelCatalogForCredentialAccount(parent),
		state:       account.ModelCatalogState(),
		mapping:     configuredModelMapping(account),
		members:     make(map[string][]AccountCatalogModel),
		implemented: make(map[string][][]string),
		discovered:  make(map[string]bool),
	}
	if evidence.catalog != nil {
		for _, model := range evidence.catalog.DiscoveredModels {
			evidence.discovered[model] = true
		}
		for _, member := range evidence.catalog.EligibleModels {
			model := member.ID
			if hasLegacyModelCatalogEvidence(evidence.catalog, member) {
				model = legacyModelCatalogWireModel(account, model)
			}
			evidence.members[model] = append(evidence.members[model], member)
			evidence.implemented[model] = append(evidence.implemented[model], accountCatalogModelEndpoints(account, member.ID, UpstreamModelMetadata{SupportedMethods: member.Endpoints}))
		}
	}
	account.GetModelMapping()
	account.GetUpstreamModelMetadataSnapshot()
	account.modelCatalogReadEvidence = evidence
}

func (gateway *GatewayService) catalogReadAccounts(ctx context.Context, key string, read func() ([]Account, error)) ([]Account, error) {
	revision, frozen := ctx.Value(visibleModelRevisionKey{}).(visibleModelRevision)
	workspace, scoped := domain.WorkspaceScope(ctx)
	cacheKey := fmt.Sprintf("catalog-evidence:%t:%d:%d:%s:%d:%s", scoped, workspace, revision.generation, ModelRegistrySnapshotFromContext(ctx).Version(), xai.RuntimeModelMappingVersion(), key)
	load := func() ([]Account, error) {
		accounts, err := read()
		if err != nil {
			return nil, err
		}
		parents := make(map[int64]*Account)
		// The batch SQL also returns credential owners, including owners that
		// have no membership in the visible groups.
		for index := range accounts {
			if !accounts[index].IsShadow() {
				prepareCatalogReadAccount(&accounts[index], nil)
				parents[accounts[index].ID] = &accounts[index]
			}
		}
		prepared := make([]Account, 0, len(accounts))
		for index := range accounts {
			account := &accounts[index]
			var parent *Account
			if account.IsShadow() {
				var loaded bool
				parent, loaded = parents[*account.ParentAccountID]
				if !loaded {
					parent, err = loadCatalogCredentialAccount(ctx, gateway.accountRepo, account)
					if err != nil {
						if !errors.Is(err, ErrAccountNotFound) {
							return nil, err
						}
						// Match the existing list behavior: a missing owner
						// makes only that shadow ineligible.
						parents[*account.ParentAccountID] = nil
						continue
					}
					if parent != nil {
						prepareCatalogReadAccount(parent, nil)
					}
					parents[*account.ParentAccountID] = parent
				}
			}
			if account.modelCatalogReadEvidence == nil {
				prepareCatalogReadAccount(account, parent)
			}
			prepared = append(prepared, *account)
		}
		if err := (&VisibleModelResolver{gateway: gateway}).validate(ctx); err != nil {
			return nil, err
		}
		return prepared, nil
	}
	// Legacy adapters without an authoritative generation cannot safely share.
	if !frozen || !revision.authoritative || gateway.modelsListCache == nil {
		return load()
	}
	if cached, found := gateway.modelsListCache.Get(cacheKey); found {
		return append([]Account(nil), cached.([]Account)...), nil
	}
	result := gateway.catalogSnapshotFlight.DoChan(cacheKey, func() (any, error) {
		if cached, found := gateway.modelsListCache.Get(cacheKey); found {
			return cached, nil
		}
		accounts, err := load()
		if err != nil {
			return nil, err
		}
		// TTL bounds memory only. Every request verifies the database generation;
		// account expiry is checked during every binding, even with a warm snapshot.
		expiration := time.Minute
		now := time.Now()
		for _, account := range accounts {
			for _, declaration := range account.ModelAvailabilityDeclarations() {
				for _, boundary := range []*time.Time{&declaration.ConfirmedAt, declaration.ExpiresAt} {
					if boundary != nil && boundary.After(now) && boundary.Sub(now) < expiration {
						expiration = boundary.Sub(now)
					}
				}
			}
		}
		gateway.modelsListCache.Set(cacheKey, accounts, expiration)
		return accounts, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case completed := <-result:
		if completed.Err != nil {
			return nil, completed.Err
		}
		return append([]Account(nil), completed.Val.([]Account)...), nil
	}
}

func (resolver *VisibleModelResolver) preloadCatalogGroups(ctx context.Context, groups []Group) (context.Context, error) {
	if resolver == nil || resolver.gateway == nil {
		return ctx, nil
	}
	reader, supported := resolver.gateway.accountRepo.(ModelCatalogBatchAccountRepository)
	if !supported {
		return ctx, nil
	}
	groupIDs := make([]int64, 0, len(groups))
	snapshot := &catalogGroupSnapshot{accounts: make(map[int64][]Account), groups: make(map[int64]*Group)}
	for index := range groups {
		group := &groups[index]
		groupIDs = append(groupIDs, group.ID)
		snapshot.groups[group.ID] = group
	}
	sort.Slice(groupIDs, func(first, second int) bool { return groupIDs[first] < groupIDs[second] })
	accounts, err := resolver.gateway.catalogReadAccounts(ctx, fmt.Sprintf("groups:%v", groupIDs), func() ([]Account, error) {
		return reader.ListModelCatalogAccountsForGroups(ctx, groupIDs)
	})
	if err != nil {
		return ctx, err
	}
	for _, account := range accounts {
		if !account.IsActive() || !account.Schedulable {
			continue
		}
		for _, groupID := range account.GroupIDs {
			group, requested := snapshot.groups[groupID]
			if !requested {
				continue
			}
			if required := group.RequiredAccountType(); required != "" && account.Type != required {
				continue
			}
			if group.Platform != "" && group.Platform != PlatformComposite && !account.MatchesPlatform(group.Platform, group.RequiredAccountType()) {
				continue
			}
			snapshot.accounts[groupID] = append(snapshot.accounts[groupID], account)
		}
	}
	return context.WithValue(ctx, catalogGroupSnapshotKey{}, snapshot), nil
}
