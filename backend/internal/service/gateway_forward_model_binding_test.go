package service

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"strings"
	"testing"
)

type forwardCatalogRepo struct {
	AccountRepository
	current *Account
	parent  *Account
	err     error
	reads   int
}

func (r *forwardCatalogRepo) GetModelCatalogAccount(_ context.Context, id int64) (*Account, error) {
	r.reads++
	if r.err != nil {
		return nil, r.err
	}
	if r.parent != nil && r.parent.ID == id {
		return r.parent, nil
	}
	return r.current, nil
}
func (r *forwardCatalogRepo) ListModelCatalogAccounts(context.Context, *int64, []string, bool) ([]Account, error) {
	if r.err != nil {
		return nil, r.err
	}
	return []Account{*r.current}, nil
}

func TestForwardModelFinalBindingAndRevocation(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "native")
	account.Credentials["model_mapping"] = map[string]any{"alias": "native", "native": "wrong"}
	current := account
	repo := &forwardCatalogRepo{current: &current}
	plan := NewGatewayRequestPlan(GatewayRequestSpec{Model: "alias"})
	ctx := WithGatewayRequestPlan(WithAffinityBindingState(context.Background()), plan)
	admitted := 0
	deferGatewayRequestBinding(ctx, 1, func(ctx context.Context) error { require.True(t, AffinityAdmitted(ctx)); admitted++; return nil })
	require.NoError(t, validateAccountForwardModel(ctx, repo, &account, "native", "responses"))
	require.Equal(t, 1, admitted, "final model must not be remapped to wrong")
	current.Extra = map[string]any{UpstreamModelCatalogStateExtraKey: AccountModelCatalogState{IdentityRevision: 2}}
	require.ErrorIs(t, validateAccountForwardModel(ctx, repo, &account, "native", "responses"), ErrAccountModelUnsupported)
	require.Equal(t, 1, admitted, "revoked identity never admits or renews affinity")
	repo.err = errors.New("catalog unavailable")
	require.ErrorContains(t, validateAccountForwardModel(ctx, repo, &account, "native", "responses"), "catalog unavailable")
}
func TestForwardModelRequestBodyPreservedAndEndpointExact(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "native")
	repo := &forwardCatalogRepo{current: &account}
	body := `{"model":"native","input":[{"type":"reasoning","encrypted_content":"opaque"},{"type":"function_call_output","call_id":"call_original","output":"ok"}]}`
	ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{}))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://fixture.test/v1/responses", strings.NewReader(body))
	require.NoError(t, err)
	require.NoError(t, validateOpenAIForwardRequest(repo, &account, req))
	sent, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.Equal(t, body, string(sent))
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, "https://fixture.test/v1/embeddings", strings.NewReader(body))
	require.NoError(t, err)
	require.ErrorIs(t, validateOpenAIForwardRequest(repo, &account, req), ErrAccountModelUnsupported)
}
func TestForwardModelMissingCatalogNeverAdmits(t *testing.T) {
	account := Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
	ctx := WithGatewayRequestPlan(context.Background(), NewGatewayRequestPlan(GatewayRequestSpec{}))
	admitted := false
	deferGatewayRequestBinding(ctx, 1, func(context.Context) error { admitted = true; return nil })
	require.ErrorIs(t, validateAccountForwardModel(ctx, &forwardCatalogRepo{current: &account}, &account, "gpt-5.1", "responses"), ErrAccountModelUnsupported)
	require.False(t, admitted)
}
func TestForwardModelPublicCatalogUsesSameEligibility(t *testing.T) {
	account := catalogEvidenceAccount(1, PlatformOpenAI, "native")
	account.Credentials["model_mapping"] = map[string]any{"alias": "native", "unsupported": "missing"}
	repo := &forwardCatalogRepo{current: &account}
	svc := &GatewayService{accountRepo: repo}
	ids, err := svc.CatalogModelIDs(context.Background(), nil, PlatformOpenAI)
	require.NoError(t, err)
	require.Contains(t, ids, "alias")
	require.NotContains(t, ids, "unsupported")
	account.Extra = nil
	ids, err = svc.CatalogModelIDs(context.Background(), nil, PlatformOpenAI)
	require.NoError(t, err)
	require.NotNil(t, ids)
	require.Empty(t, ids)
}
