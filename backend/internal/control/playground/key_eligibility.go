package playground

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/scope"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	keycompat "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/legacy/identity"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/catalog"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
	pg "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/playground"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/ip"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

type selectedKey struct {
	ctx     context.Context
	scope   scope.Authorized
	key     *service.APIKey
	models  service.ModelAccessSnapshot
	actions []string
	digest  string
}

var keyIneligible = &fault.Fault{Status: 403, Reason: "KEY_INELIGIBLE", Message: "The selected key cannot run in this project."}

// checkSelectedKey is shared by model discovery, preparation and dispatch.
// An ineligible key remains discoverable to an authorized owner or delegate.
func (g *Gateway) checkSelectedKey(ctx context.Context, a identity.Access, keyID, payer int64, invocation bool) (selectedKey, error) {
	var selected selectedKey
	if g == nil || g.DB == nil || g.Keys == nil || g.Scopes == nil || g.Access == nil {
		return selected, pg.Unavailable
	}
	ctx = domain.WithWorkspaceScope(ctx, a.WorkspaceID)
	var owner int64
	var group sql.NullInt64
	var project, exhausted, groupEligible bool
	var digest sql.NullString
	var status string
	var expires *time.Time
	var databaseTime time.Time
	err := g.DB.QueryRowContext(ctx, `SELECT k.user_id,k.group_id,issued.api_key_id IS NOT NULL,k.status,k.expires_at,
 now(),k.quota>0 AND k.quota_used>=k.quota,credential.credential_digest,
 EXISTS (
   SELECT 1 FROM groups g JOIN users u ON u.id=k.user_id AND u.status='active' AND u.deleted_at IS NULL
   LEFT JOIN project_resources gr ON gr.kind='groups' AND gr.resource_id=g.id
   WHERE g.id=k.group_id AND g.workspace_id=k.workspace_id AND g.status='active' AND g.deleted_at IS NULL
     AND (gr.resource_id IS NULL OR gr.workspace_id=g.workspace_id)
     AND coalesce(gr.project_id,default_project.id)=key_project.id
     AND NOT EXISTS (SELECT 1 FROM identity_scope_quarantine q WHERE q.kind='groups' AND q.resource_id=g.id)
     AND `+identity.KeyGroupEntitlement+`
 )
 FROM api_keys k`+identity.KeyAttributionJoins+`
 LEFT JOIN LATERAL (
   SELECT c.credential_digest FROM api_key_credentials c WHERE `+identity.CurrentKeyCredential+`
   ORDER BY c.id DESC LIMIT 1
 ) credential ON true
 WHERE k.id=$1 AND k.workspace_id=$2 AND key_project.id=$3 AND k.deleted_at IS NULL AND `+identity.ValidKeyAttribution,
		keyID, a.WorkspaceID, a.ProjectID).Scan(&owner, &group, &project, &status, &expires, &databaseTime, &exhausted, &digest, &groupEligible)
	if errors.Is(err, sql.ErrNoRows) {
		return selected, fault.NotFound
	}
	if err != nil {
		return selected, err
	}
	if owner != a.ActorID && (a.Require("keys.act_as") != nil || invocation && payer != owner) {
		return selected, fault.NotFound
	}
	if payer != 0 && payer != owner {
		return selected, fault.Invalid("/payer_user_id")
	}
	if state := identity.KeyLifecycle(status, expires, databaseTime, exhausted, digest.Valid); state != identity.KeyActive {
		if invocation {
			switch state {
			case identity.KeyExpired:
				return selected, &fault.Fault{Status: 403, Reason: "API_KEY_EXPIRED", Message: "API key has expired"}
			case identity.KeyQuotaExhausted:
				return selected, &fault.Fault{Status: 429, Reason: "API_KEY_QUOTA_EXHAUSTED", Message: "API key quota exhausted"}
			}
		}
		return selected, keyIneligible
	}
	if !group.Valid || !groupEligible {
		return selected, keyIneligible
	}
	var base scope.Authorized
	var issued *identity.KeyPolicy
	if project {
		issued, err = identity.ResolveProjectCredential(ctx, g.Scopes, digest.String)
		if errors.Is(err, identity.ErrKeyAttribution) {
			return selected, fault.NotFound
		}
		if errors.Is(err, fault.Forbidden) || errors.Is(err, fault.NotFound) || err == nil && issued == nil {
			return selected, keyIneligible
		}
		if err != nil {
			return selected, err
		}
		if issued.GroupID != group.Int64 || len(issued.Actions) == 0 {
			return selected, keyIneligible
		}
		base, selected.actions = issued.Scope, issued.Actions
	} else {
		base, err = identity.ResolveLegacyKeyScope(ctx, g.Scopes, a.WorkspaceID, keyID, owner, digest.String)
		if errors.Is(err, fault.NotFound) || errors.Is(err, fault.Forbidden) {
			return selected, keyIneligible
		}
		if err != nil {
			return selected, err
		}
	}
	if base.WorkspaceID != a.WorkspaceID || base.ProjectID != a.ProjectID || base.KeyID != keyID ||
		base.KeyOwnerID != owner || base.FundingUserID != owner || base.Version != a.Version {
		return selected, fault.NotFound
	}
	ownerAccess, err := g.Access.Evaluate(ctx, owner, a.WorkspaceID, a.ProjectID, "")
	if errors.Is(err, fault.NotFound) || errors.Is(err, fault.Forbidden) {
		return selected, keyIneligible
	}
	if err != nil {
		return selected, err
	}
	if ownerAccess.Version != a.Version {
		return selected, fault.Forbidden
	}
	if project {
		selected.actions, err = identity.EffectiveKeyActions(selected.actions, ownerAccess)
		if err != nil {
			return selected, keyIneligible
		}
	} else {
		selected.actions = ownerAccess.Actions
	}
	if issued != nil {
		selected.key, err = keycompat.ProjectKey(ctx, g.Keys, issued)
	} else {
		selected.key, err = g.Keys.GetByID(ctx, keyID)
	}
	if errors.Is(err, service.ErrAPIKeyNotFound) {
		return selected, fault.NotFound
	}
	if err != nil {
		return selected, err
	}
	k := selected.key
	if k == nil || k.ID != keyID || k.WorkspaceID != a.WorkspaceID || k.UserID != owner || k.User == nil ||
		k.User.ID != owner || !k.User.IsActive() || k.GroupID == nil || *k.GroupID != group.Int64 ||
		k.Group == nil || k.Group.ID != group.Int64 || !k.Group.IsActive() {
		return selected, keyIneligible
	}
	if state := identity.KeyLifecycle(k.Status, k.ExpiresAt, databaseTime, k.IsQuotaExhausted(), true); state != identity.KeyActive {
		return selected, keyIneligible
	}
	base.ActorID = a.ActorID
	selected.ctx, err = g.Keys.AttachModelAccessPolicy(scope.With(ctx, base), keyID)
	if errors.Is(err, service.ErrModelAccessNotFound) {
		return selected, fault.NotFound
	}
	if err != nil {
		return selected, err
	}
	var loaded bool
	selected.models, loaded = service.ModelAccessFromContext(selected.ctx)
	if !loaded || selected.models.RestrictedKey != project {
		return selected, pg.Unavailable
	}
	selected.scope, selected.digest = base, digest.String
	return selected, nil
}

func (g *Gateway) checkSelectedFunding(ctx context.Context, key *service.APIKey, clientIP string) (*service.UserSubscription, error) {
	if len(key.IPWhitelist)+len(key.IPBlacklist) > 0 {
		if ok, _ := ip.CheckIPRestrictionWithCompiledRules(clientIP, key.CompiledIPWhitelist, key.CompiledIPBlacklist); !ok {
			return nil, &fault.Fault{Status: 403, Reason: "ACCESS_DENIED", Message: "Access denied"}
		}
	}
	if key.Group.IsSubscriptionType() && g.Subscriptions == nil {
		return nil, pg.Unavailable
	}
	if !key.Group.IsSubscriptionType() && key.User.Balance <= 0 {
		plan := service.GatewayRequestPlanFromContext(ctx)
		held, err := g.currentWalletHold(ctx, key, plan.BudgetHold())
		if err != nil || held {
			return nil, err
		}
	}
	return keycompat.CheckRunFunding(ctx, key, g.Subscriptions)
}

func (g *Gateway) currentWalletHold(ctx context.Context, key *service.APIKey, hold *service.KeyBudgetReservation) (bool, error) {
	if hold == nil || !hold.BalanceHeld || hold.APIKeyID != key.ID || hold.UserID != key.UserID {
		return false, nil
	}
	var held bool
	err := g.DB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM api_key_budget_reservations
 WHERE id=$1 AND api_key_id=$2 AND user_id=$3 AND request_id=$4
 AND status='reserved' AND balance_held AND hold_amount>0 AND expires_at>now())`,
		hold.ID, key.ID, key.UserID, hold.RequestID).Scan(&held)
	return held, err
}

type modelPair struct {
	Action   string `json:"action"`
	Protocol string `json:"protocol"`
}

func supportedPair(pair modelPair) bool {
	return pair.Action == "text.chat" && slices.Contains([]string{"openai_chat", "openai_responses", "anthropic_messages", "gemini_generate"}, pair.Protocol)
}

type groupCatalog struct {
	snapshot catalog.Snapshot
	byID     map[string]string
	byName   map[string]string
	accounts map[string]int64
}

func (g *Gateway) selectedCatalog(ctx context.Context, a identity.Access, key selectedKey, asOf time.Time) (groupCatalog, error) {
	var result groupCatalog
	if g.Catalog == nil {
		return result, pg.Unavailable
	}
	snap, err := g.Catalog.Snapshot(catalog.WithoutSalePrices(ctx), catalog.Perspective{
		Actor: key.scope.FundingUserID, Workspace: a.WorkspaceID, Project: a.ProjectID,
		AccessVersion: a.Version, EndpointMetadata: true,
	}, asOf)
	if errors.Is(err, fault.NotFound) {
		return result, pg.Unavailable
	}
	if err != nil {
		return result, err
	}
	result = groupCatalog{snapshot: snap, byID: map[string]string{}, byName: map[string]string{}}
	group := key.key.Group.ID
	inGroup := func(m catalog.Detail) bool { return slices.Contains(m.EligibleGroups, group) }
	result.snapshot.Details = slices.DeleteFunc(slices.Clone(snap.Details), func(m catalog.Detail) bool {
		return !inGroup(m) || m.Model.AccessState != "allowed" || m.Model.RouteState != "eligible"
	})
	for _, m := range result.snapshot.Details {
		result.byID[m.Model.ID] = m.Model.ID
		for _, name := range append(slices.Clone(m.Aliases), m.Model.Model) {
			if id, found := result.byName[name]; found && id != m.Model.ID {
				result.byName[name] = ""
			} else {
				result.byName[name] = m.Model.ID
			}
		}
	}
	result.accounts, err = g.eligibleAccounts(ctx, a, group)
	return result, err
}

func (c groupCatalog) resolve(model string) string {
	if id, ok := c.byID[model]; ok {
		return id
	}
	return c.byName[model]
}

func (c groupCatalog) matches(m catalog.Detail) func(string) bool {
	return func(entry string) bool { return m.Model.ID != "" && c.resolve(entry) == m.Model.ID }
}

func (c groupCatalog) excludes(m catalog.Detail) func(string) bool {
	return func(entry string) bool {
		if id, exists := c.byID[entry]; exists {
			return id == m.Model.ID
		}
		return entry == m.Model.Model || slices.Contains(m.Aliases, entry)
	}
}

func (c groupCatalog) endpoints(key selectedKey, m catalog.Detail, pair modelPair, now time.Time, playground bool) []catalog.Endpoint {
	matches, excludes := c.matches(m), c.excludes(m)
	if playground && !supportedPair(pair) || !slices.Contains(key.actions, pair.Action) || !key.models.AllowsKeyModel(m.Model.Model, matches, excludes) {
		return nil
	}
	var endpoints []catalog.Endpoint
	for _, endpoint := range m.Endpoints {
		account, ok := c.accounts[endpoint.ID]
		if !ok || !key.models.AllowsCatalogAccountModel(account, matches, excludes) {
			continue
		}
		if slices.ContainsFunc(endpoint.Capabilities, func(cap catalog.Capability) bool {
			return cap.Action == pair.Action && cap.Protocol == pair.Protocol && cap.State == "fresh" &&
				cap.VerifiedAt != nil && !cap.VerifiedAt.After(now) && (cap.ExpiresAt == nil || cap.ExpiresAt.After(now)) &&
				(!playground || cap.Stream != "unsupported")
		}) {
			endpoints = append(endpoints, endpoint)
		}
	}
	return endpoints
}

func (g *Gateway) checkRunModel(ctx context.Context, a identity.Access, in pg.Submit, asOf time.Time) (selectedKey, groupCatalog, catalog.Detail, error) {
	key, err := g.checkSelectedKey(ctx, a, in.Key, in.Payer, true)
	if err != nil {
		return key, groupCatalog{}, catalog.Detail{}, err
	}
	models, model, err := g.checkSelectedModel(ctx, a, key, in.Model, modelPair{in.Action, in.Protocol}, asOf, true)
	return key, models, model, err
}

type catalogKeyContextKey struct{}

type catalogKeySelection struct {
	access identity.Access
	key selectedKey
	accounts map[int64]struct{}
}

// AuthorizeCatalogKey reuses the same live credential, group and model
// policy as Playground. Catalog acquisition receives the actual payer.
func (g *Gateway) AuthorizeCatalogKey(ctx context.Context, p catalog.Perspective) (context.Context, int64, error) {
	a, key, err := g.catalogKey(ctx, p, p.KeyID, 0)
	if err != nil {
		return ctx, 0, err
	}
	group := key.key.Group.ID
	endpoints, err := g.eligibleAccounts(key.ctx, a, group)
	if err != nil {
		return ctx, 0, err
	}
	accounts := make(map[int64]struct{}, len(endpoints))
	for _, account := range endpoints {
		accounts[account] = struct{}{}
	}
	selection := catalogKeySelection{access: a, key: key, accounts: accounts}
	return context.WithValue(key.ctx, catalogKeyContextKey{}, selection), group, nil
}

func (g *Gateway) CatalogKeyAllows(ctx context.Context, binding catalog.ModelBinding) bool {
	selection, ok := ctx.Value(catalogKeyContextKey{}).(catalogKeySelection)
	if !ok || binding.BillingGroupID != selection.key.key.Group.ID ||
		!slices.Contains(selection.key.actions, binding.Action) {
		return false
	}
	if _, ok := selection.accounts[binding.AccountID]; !ok {
		return false
	}
	// A bare name cannot identify a provider inside a composite group.
	// An exclusion by bare name still vetoes the binding.
	id := binding.Identity
	matches := func(entry string) bool {
		return entry == id.ID() || selection.key.key.Group.Platform != service.PlatformComposite && entry == id.Model
	}
	excludes := func(entry string) bool { return entry == id.ID() || entry == id.Model }
	return selection.key.models.AllowsKeyModel(id.Model, matches, excludes) &&
		selection.key.models.AllowsCatalogAccountModel(binding.AccountID, matches, excludes)
}

func (g *Gateway) ValidateCatalogKey(ctx context.Context) error {
	selection, ok := ctx.Value(catalogKeyContextKey{}).(catalogKeySelection)
	if !ok {
		return pg.Unavailable
	}
	return g.verifySelectionRevision(ctx, selection.access, selection.key)
}

// AuthorizeSelectedModel is the read-only port for selected-key quotes and
// comparisons. Price lookup uses the returned payer and canonical model.
func (g *Gateway) AuthorizeSelectedModel(ctx context.Context, p catalog.Perspective, keyID, payer int64, model, action, protocol string, asOf time.Time) (scope.Authorized, catalog.Detail, error) {
	a, key, err := g.catalogKey(ctx, p, keyID, payer)
	if err != nil {
		return scope.Authorized{}, catalog.Detail{}, err
	}
	models, detail, err := g.checkSelectedModel(ctx, a, key, model, modelPair{action, protocol}, asOf, false)
	if err != nil {
		return scope.Authorized{}, catalog.Detail{}, err
	}
	accounts := make(map[int64]bool, len(detail.Endpoints))
	for _, ep := range detail.Endpoints {
		accounts[models.accounts[ep.ID]] = true
	}
	detail.Offers = slices.Clone(detail.Offers)
	for i := range detail.Offers {
		offer := &detail.Offers[i]
		offer.Bindings = slices.DeleteFunc(slices.Clone(offer.Bindings), func(b catalog.ModelBinding) bool {
			return !accounts[b.AccountID] || b.BillingGroupID != key.key.Group.ID
		})
	}
	return key.scope, detail, nil
}

func (g *Gateway) catalogKey(ctx context.Context, p catalog.Perspective, keyID, payer int64) (identity.Access, selectedKey, error) {
	if p.Public() || keyID <= 0 {
		return identity.Access{}, selectedKey{}, fault.NotFound
	}
	if g == nil || g.Access == nil {
		return identity.Access{}, selectedKey{}, pg.Unavailable
	}
	a, err := g.Access.Evaluate(ctx, p.Actor, p.Workspace, p.Project, "models.read")
	if err != nil {
		return a, selectedKey{}, err
	}
	if a.Version != p.AccessVersion {
		return a, selectedKey{}, selectionChanged
	}
	if err := a.Require("keys.read"); err != nil {
		return a, selectedKey{}, err
	}
	key, err := g.checkSelectedKey(ctx, a, keyID, payer, false)
	return a, key, err
}

func (g *Gateway) checkSelectedModel(ctx context.Context, a identity.Access, key selectedKey, modelID string, pair modelPair, asOf time.Time, playground bool) (groupCatalog, catalog.Detail, error) {
	if !slices.Contains(key.actions, pair.Action) {
		return groupCatalog{}, catalog.Detail{}, fault.Forbidden
	}
	if key.models.DeniesAllKeyModels() {
		return groupCatalog{}, catalog.Detail{}, unavailableModel
	}
	models, err := g.selectedCatalog(ctx, a, key, asOf)
	if err != nil {
		return models, catalog.Detail{}, err
	}
	id := models.resolve(modelID)
	for _, model := range models.snapshot.Details {
		if model.Model.ID != id {
			continue
		}
		model.Endpoints = models.endpoints(key, model, pair, time.Now(), playground)
		if len(model.Endpoints) != 0 {
			return models, model, g.verifySelectionRevision(ctx, a, key)
		}
		break
	}
	return models, catalog.Detail{}, unavailableModel
}

// Recheck the revisions and credential after catalog acquisition. Rotation
// and concurrent edits must not leak a page assembled from obsolete facts.
func (g *Gateway) verifySelectionRevision(ctx context.Context, a identity.Access, key selectedKey) error {
	var current bool
	err := g.DB.QueryRowContext(ctx, `SELECT EXISTS (
   SELECT 1 FROM api_keys k`+identity.KeyAttributionJoins+`
   JOIN workspaces w ON w.id=k.workspace_id
   JOIN model_catalog_generations gen ON gen.scope='catalog'
   WHERE k.id=$1 AND w.id=$2 AND key_project.id=$3 AND w.scope_version=$4 AND gen.generation=$5
   AND k.deleted_at IS NULL AND k.group_id=$6 AND k.user_id=$7
   AND k.status='active' AND (k.expires_at IS NULL OR k.expires_at>now()) AND (k.quota<=0 OR k.quota_used<k.quota)
   AND `+identity.ValidKeyAttribution+`
   AND EXISTS (SELECT 1 FROM api_key_credentials c WHERE `+identity.CurrentKeyCredential+` AND c.credential_digest=$8)
 )`, key.key.ID, a.WorkspaceID, a.ProjectID, a.Version, key.models.Generation, key.key.Group.ID, key.key.UserID, key.digest).Scan(&current)
	if err != nil {
		return err
	}
	if !current {
		return selectionChanged
	}
	return nil
}

func (g *Gateway) eligibleAccounts(ctx context.Context, a identity.Access, group int64) (map[string]int64, error) {
	rows, err := g.DB.QueryContext(ctx, `SELECT pc.account_id FROM provider_connections pc
 JOIN account_groups ag ON ag.account_id=pc.account_id AND ag.group_id=$2
 JOIN accounts ac ON ac.id=pc.account_id AND ac.workspace_id=pc.workspace_id AND ac.deleted_at IS NULL AND ac.status='active' AND ac.schedulable
 JOIN projects d ON d.workspace_id=pc.workspace_id AND d.is_default
 LEFT JOIN project_resources r ON r.kind='accounts' AND r.resource_id=pc.account_id
 JOIN connection_verifications v ON v.workspace_id=pc.workspace_id AND v.account_id=pc.account_id
   AND v.id=pc.verified_id AND v.base_version<=pc.version AND v.eligible AND v.expires_at>now()
 JOIN connection_credential_versions cv ON cv.workspace_id=v.workspace_id AND cv.account_id=v.account_id
   AND cv.version=v.credential_version AND cv.revoked_at IS NULL
 WHERE pc.workspace_id=$1 AND (r.resource_id IS NULL OR r.workspace_id=pc.workspace_id)
   AND coalesce(r.project_id,d.id)=$3 AND pc.state='active' AND NOT pc.legacy_dirty
   AND NOT EXISTS (SELECT 1 FROM identity_scope_quarantine q WHERE q.kind='accounts' AND q.resource_id=pc.account_id)
   AND NOT EXISTS (SELECT 1 FROM connection_migration_quarantine q WHERE q.account_id=pc.account_id)
   AND ((pc.proxy_id IS NULL AND v.proxy_revision='') OR EXISTS (
     SELECT 1 FROM proxies p WHERE p.id=pc.proxy_id AND p.workspace_id=pc.workspace_id AND p.status='active'
       AND p.deleted_at IS NULL AND (p.expires_at IS NULL OR p.expires_at>now()) AND p.updated_at::text=v.proxy_revision))`,
		a.WorkspaceID, group, a.ProjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := map[string]int64{}
	for rows.Next() {
		var account int64
		if err := rows.Scan(&account); err != nil {
			return nil, err
		}
		accounts["ep_"+catalog.Digest([2]int64{a.ProjectID, account})[:32]] = account
	}
	return accounts, rows.Err()
}
