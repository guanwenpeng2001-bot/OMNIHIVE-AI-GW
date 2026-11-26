//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeModelSelection(t *testing.T) {
	restricted := AccountModelAllowlist{Enabled: true, Models: []string{"existing"}}
	current := map[string]string{"a": "old-target"}
	cases := []struct {
		name         string
		scope        AccountModelAllowlist
		mapping      map[string]string
		cmd          ModelSelectionCommand
		mode         ModelSelectionMode
		models, auto []string
		err          error
	}{
		{name: "scope without mapping", cmd: ModelSelectionCommand{Mode: ModelSelectionRestricted, Models: []string{"B", "a", "B"}}, mode: ModelSelectionRestricted, models: []string{"B", "a"}},
		{name: "target union", mapping: current, cmd: ModelSelectionCommand{Mode: ModelSelectionRestricted, Models: []string{"manual"}}, mode: ModelSelectionRestricted, models: []string{"manual", "old-target"}, auto: []string{"old-target"}},
		{name: "mapping only changes restricted scope", scope: restricted, mapping: current, cmd: ModelSelectionCommand{ModelMapping: map[string]string{"a": "new", "b*": "new"}}, mode: ModelSelectionRestricted, models: []string{"existing", "new"}, auto: []string{"new"}},
		{name: "mapping only starts restriction", cmd: ModelSelectionCommand{ModelMapping: map[string]string{"a": "native"}}, mode: ModelSelectionRestricted, models: []string{"native"}, auto: []string{"native"}},
		{name: "inactive scope members do not become active", scope: AccountModelAllowlist{Models: []string{"disabled"}}, cmd: ModelSelectionCommand{ModelMapping: map[string]string{"a": "native"}}, mode: ModelSelectionRestricted, models: []string{"native"}, auto: []string{"native"}},
		{name: "mapping clear preserves scope", scope: restricted, mapping: current, cmd: ModelSelectionCommand{ModelMapping: map[string]string{}}, mode: ModelSelectionRestricted, models: []string{"existing"}},
		{name: "explicit inherit preserves mapping", scope: restricted, mapping: current, cmd: ModelSelectionCommand{Mode: ModelSelectionInherit}, mode: ModelSelectionInherit},
		{name: "deny retains mapping", scope: restricted, mapping: current, cmd: ModelSelectionCommand{Mode: ModelSelectionDenyAll, ActorID: 1, Reason: "maintenance"}, mode: ModelSelectionDenyAll},
		{name: "missing reason", cmd: ModelSelectionCommand{Mode: ModelSelectionDenyAll, ActorID: 1, Reason: " "}, err: ErrModelSelectionReasonRequired},
		{name: "deny without actor", cmd: ModelSelectionCommand{Mode: ModelSelectionDenyAll, Reason: "maintenance"}, err: ErrModelSelectionInvalid},
		{name: "deny with mapping", cmd: ModelSelectionCommand{Mode: ModelSelectionDenyAll, ActorID: 1, Reason: "maintenance", ModelMapping: current}, err: ErrModelSelectionInvalid},
		{name: "mapping cannot unlock", scope: AccountModelAllowlist{Enabled: true}, cmd: ModelSelectionCommand{ModelMapping: current}, err: ErrModelSelectionDenied},
		{name: "wildcard target", cmd: ModelSelectionCommand{Mode: ModelSelectionRestricted, ModelMapping: map[string]string{"a*": "b*"}}, err: ErrModelSelectionInvalid},
		{name: "empty restriction", cmd: ModelSelectionCommand{Mode: ModelSelectionRestricted}, err: ErrModelSelectionInvalid},
	}
	for _, fixture := range cases {
		t.Run(fixture.name, func(t *testing.T) {
			result, err := NormalizeModelSelection(fixture.scope, fixture.mapping, fixture.cmd)
			if fixture.err != nil {
				require.ErrorIs(t, err, fixture.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, fixture.mode, result.Mode)
			require.Equal(t, append([]string{}, fixture.models...), result.Models)
			require.Equal(t, append([]string{}, fixture.auto...), result.AutoAdded)
			expectedMapping := fixture.mapping
			if fixture.cmd.ModelMapping != nil {
				expectedMapping = fixture.cmd.ModelMapping
			}
			require.Equal(t, copySelectionMapping(expectedMapping), result.ModelMapping)
			result.ModelMapping["a"] = "changed copy"
			require.Equal(t, map[string]string{"a": "old-target"}, current)
		})
	}
	require.Equal(t, []string{"existing"}, restricted.Models)
}

func TestResolveModelSelectionMode(t *testing.T) {
	enabled, disabled := true, false
	for _, fixture := range []struct {
		mode    ModelSelectionMode
		enabled *bool
		models  []string
		confirm bool
		want    ModelSelectionMode
		err     error
	}{
		{enabled: &enabled, models: []string{"a"}, want: ModelSelectionRestricted},
		{enabled: &disabled, models: []string{"a"}, want: ModelSelectionRestricted},
		{enabled: &disabled, want: ModelSelectionInherit},
		{enabled: &enabled, err: ErrModelSelectionConfirmationRequired},
		{enabled: &enabled, confirm: true, want: ModelSelectionDenyAll},
		{mode: ModelSelectionDenyAll, want: ModelSelectionDenyAll},
		{mode: ModelSelectionDenyAll, enabled: &disabled, err: ErrModelSelectionInvalid},
	} {
		mode, err := ResolveModelSelectionMode(fixture.mode, fixture.enabled, fixture.models, fixture.confirm)
		require.ErrorIs(t, err, fixture.err)
		require.Equal(t, fixture.want, mode)
	}
}

type selectionTestRepository struct {
	AccountRepository
	account Account
	writes  int
}

func (repo *selectionTestRepository) UpdateModelSelection(_ context.Context, id int64, _ ModelSelectionSource, resolve func(*Account) (ModelSelectionResult, error)) (ModelSelectionResult, error) {
	if id != repo.account.ID {
		return ModelSelectionResult{}, ErrAccountNotFound
	}
	result, err := resolve(&repo.account)
	if err != nil || !result.Changed {
		return result, err
	}
	repo.writes++
	repo.account.Extra[UpstreamModelAllowlistExtraKey] = result.Scope
	state := repo.account.ModelCatalogState()
	state.RulesRevision = result.RulesRevision
	repo.account.Extra[UpstreamModelCatalogStateExtraKey] = state
	if result.MappingChanged {
		repo.account.Credentials["model_mapping"] = result.ModelMapping
	}
	return result, nil
}

func TestSaveModelSelectionUsesLockedBaseline(t *testing.T) {
	repo := &selectionTestRepository{account: Account{
		ID: 7, Credentials: map[string]any{"model_mapping": map[string]string{"alias": "locked"}},
		Extra: map[string]any{UpstreamModelCatalogStateExtraKey: AccountModelCatalogState{RulesRevision: 4, IdentityRevision: 2}},
	}}
	svc := &AccountTestService{accountRepo: repo}
	revision, identity := int64(4), int64(2)
	cmd := ModelSelectionCommand{Mode: ModelSelectionRestricted, Models: []string{"manual"}, ActorID: 71, ExpectedRulesRevision: &revision, ExpectedIdentityRevision: &identity}
	result, err := svc.SaveModelSelection(context.Background(), 7, cmd)
	require.NoError(t, err)
	require.Equal(t, []string{"locked", "manual"}, result.Models)
	require.Equal(t, []string{"locked"}, result.AutoAdded)
	require.Equal(t, result.Models, result.NotDiscovered)
	require.EqualValues(t, 5, result.RulesRevision)
	require.EqualValues(t, 71, result.Scope.ConfirmedBy)
	require.EqualValues(t, 2, result.Scope.IdentityRevision)
	require.Equal(t, 1, repo.writes)
	_, err = svc.SaveModelSelection(context.Background(), 7, cmd)
	require.ErrorIs(t, err, ErrModelSelectionConflict)
	revision++
	identity++
	_, err = svc.SaveModelSelection(context.Background(), 7, cmd)
	require.ErrorIs(t, err, ErrModelSelectionConflict)
	identity--
	result, err = svc.SaveModelSelection(context.Background(), 7, cmd)
	require.NoError(t, err)
	require.False(t, result.Changed)
	require.EqualValues(t, 5, result.RulesRevision)
	require.Equal(t, 1, repo.writes)
}

func TestInitializeAccountModelSelection(t *testing.T) {
	empty := &Account{}
	require.NoError(t, InitializeAccountModelSelection(empty))
	require.Equal(t, ModelSelectionInherit, AccountModelSelectionView(empty).Mode)
	mapped := &Account{Credentials: map[string]any{"model_mapping": map[string]string{"alias": "native"}}}
	require.NoError(t, InitializeAccountModelSelection(mapped))
	require.Equal(t, []string{"native"}, AccountModelSelectionView(mapped).Models)
	require.EqualValues(t, 0, mapped.ModelSelectionResult.RulesRevision)
}

func TestModelSelectionDenyAllPreservesOmittedMapping(t *testing.T) {
	mapping := map[string]string{"public-alias": "vendor/model-a"}
	current := AccountModelAllowlist{Enabled: true, Models: []string{"vendor/model-a"}}
	denied, err := NormalizeModelSelection(current, mapping, ModelSelectionCommand{
		Mode: ModelSelectionDenyAll, ActorID: 7, Reason: "upstream maintenance",
	})
	require.NoError(t, err)
	require.Equal(t, ModelSelectionDenyAll, denied.Mode)
	require.Empty(t, denied.Models)
	require.Equal(t, mapping, denied.ModelMapping)
	require.False(t, denied.MappingChanged)
	reopened, err := NormalizeModelSelection(denied.Scope, denied.ModelMapping, ModelSelectionCommand{
		Mode: ModelSelectionRestricted, Models: []string{"vendor/model-b"},
	})
	require.NoError(t, err)
	require.Equal(t, mapping, reopened.ModelMapping)
	require.Equal(t, []string{"vendor/model-a"}, reopened.AutoAdded)
}

func TestModelSelectionDenyAllAppliesWithPassthrough(t *testing.T) {
	account := catalogEvidenceAccount(7, PlatformOpenAI, "native")
	account.Extra["openai_passthrough"] = true
	account.Extra[UpstreamModelAllowlistExtraKey] = map[string]any{
		"enabled": true, "models": []any{}, "reason": "maintenance", "confirmed_by": 7,
	}
	require.True(t, account.ModelSelectionDeniesAll())
	require.False(t, account.IsModelSupported("native"))
	require.False(t, (&GatewayService{}).isModelSupportedByAccount(&account, "native"))
	require.False(t, ResolveAccountModelBinding(context.Background(), &account, "native", AccountModelBindingOptions{Endpoint: CompositeRouteEndpointResponses, Passthrough: true, FinalModel: "native"}).Supported)
	account.Extra[UpstreamModelAllowlistExtraKey] = AccountModelAllowlist{Enabled: true, Models: []string{}, Reason: "system_upgrade_baseline_freeze"}
	require.False(t, account.ModelSelectionDeniesAll())
}
