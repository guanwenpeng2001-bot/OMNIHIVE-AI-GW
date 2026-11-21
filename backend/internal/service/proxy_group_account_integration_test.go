//go:build unit

package service

import (
	"context"
	"errors"
	dbent "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/ent"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/openai"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
	"time"
)

func TestProxyGroupAccountChoiceClearOmitConflict(t *testing.T) {
	group, node, zero := int64(7), int64(11), int64(0)
	for _, tc := range []struct {
		name                                   string
		currentNode, currentGroup, node, group *int64
		wantNode, wantGroup                    *int64
		conflict                               bool
	}{
		{"unbound", nil, nil, nil, nil, nil, nil, false},
		{"choose group", nil, nil, &zero, &group, nil, &group, false},
		{"choose node", nil, nil, &node, &zero, &node, nil, false},
		{"clear group", nil, &group, &zero, &zero, nil, nil, false},
		{"clear node", &node, nil, &zero, &zero, nil, nil, false},
		{"omit preserves group", nil, &group, nil, nil, nil, &group, false},
		{"omit preserves node", &node, nil, nil, nil, &node, nil, false},
		{"both positive", nil, nil, &node, &group, nil, nil, true},
		{"omitted group conflicts with node", nil, &group, &node, nil, nil, &group, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &Account{ProxyID: tc.currentNode, ProxyGroupID: tc.currentGroup}
			err := applyAccountProxyChoice(a, tc.node, tc.group)
			if tc.conflict {
				require.Error(t, err)
				require.Equal(t, tc.currentNode, a.ProxyID)
				require.Equal(t, tc.currentGroup, a.ProxyGroupID)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.wantNode, a.ProxyID)
				require.Equal(t, tc.wantGroup, a.ProxyGroupID)
			}
		})
	}
}

func TestProxyGroupShadowsShareOwnerAndLogicalIdentity(t *testing.T) {
	r, _, cache, parent := runtimeFixture()
	owner := parent.ID
	shadow := *parent
	shadow.ID = 92
	shadow.ParentAccountID = &owner
	identity := AccountRuntimeIdentityKey(&shadow)
	parent, parentTarget, err := r.ApplyEgress(context.Background(), parent)
	require.NoError(t, err)
	resolved, target, err := r.ApplyEgress(context.Background(), &shadow)
	require.NoError(t, err)
	require.Equal(t, parentTarget.ProxyID, target.ProxyID)
	require.Len(t, cache.bindings, 1)
	require.Zero(t, cache.bindings[[2]int64{7, shadow.ID}])
	require.Equal(t, identity, AccountRuntimeIdentityKey(resolved))
	resolved.egress.target.ProxyID = 999
	require.Equal(t, identity, AccountRuntimeIdentityKey(resolved), "an ephemeral node never changes authentication identity")
	changed := int64(8)
	resolved.ProxyGroupID = &changed
	require.NotEqual(t, identity, AccountRuntimeIdentityKey(resolved))
}

type egressOpenAIOAuthClient struct {
	OpenAIOAuthClient
	urls []string
}

func (c *egressOpenAIOAuthClient) ExchangeCode(_ context.Context, _, _, _, proxy, _ string) (*openai.TokenResponse, error) {
	c.urls = append(c.urls, proxy)
	return &openai.TokenResponse{AccessToken: "fixture-access", RefreshToken: "fixture-rotated", ExpiresIn: 3600}, nil
}

func TestProxyGroupOAuthSessionPinsAndRejectsClientReroute(t *testing.T) {
	r, _, cache, a := runtimeFixture()
	client := &egressOpenAIOAuthClient{}
	svc := NewOpenAIOAuthService(nil, client)
	svc.egressRuntime = r
	defer svc.Stop()
	zero := int64(0)
	result, err := svc.GenerateAuthURL(context.Background(), &zero, "", openai.OAuthPlatformOpenAI, a.ProxyGroupID)
	require.NoError(t, err)
	session, ok := svc.sessionStore.Get(result.SessionID)
	require.True(t, ok)
	require.NotNil(t, session.Egress)
	require.Empty(t, cache.bindings)
	otherNode := int64(500)
	_, err = svc.ExchangeCode(context.Background(), &OpenAIExchangeCodeInput{SessionID: result.SessionID, State: session.State, Code: "fixture", ProxyID: &otherNode, ProxyGroupID: a.ProxyGroupID})
	require.ErrorContains(t, err, "PROXY_BINDING_CONFLICT")
	require.Empty(t, client.urls)
	info, err := svc.ExchangeCode(context.Background(), &OpenAIExchangeCodeInput{SessionID: result.SessionID, State: session.State, Code: "fixture", ProxyID: &zero, ProxyGroupID: a.ProxyGroupID})
	require.NoError(t, err)
	require.Equal(t, []string{session.ProxyURL}, client.urls)
	require.Equal(t, session.Egress, info.Egress)
	require.Empty(t, cache.bindings)
	state := &accountCredentialDraftState{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"refresh_token": info.RefreshToken}, Egress: info.Egress}
	draft := credentialDraftAccount(state)
	url, err := draft.EgressProxyURL(context.Background())
	require.NoError(t, err)
	require.Equal(t, session.ProxyURL, url)
	cache.fail = errors.New("cache write unavailable")
	err = r.confirmOAuthDraftBinding(context.Background(), info.Egress, 101)
	require.Error(t, err)
	require.Equal(t, "fixture-rotated", state.Credentials["refresh_token"])
	cache.fail = nil
	require.NoError(t, r.confirmOAuthDraftBinding(context.Background(), info.Egress, 101))
	require.Equal(t, info.Egress.ProxyID, cache.bindings[[2]int64{7, 101}])
}

func TestProxyGroupRefreshCASUsesLogicalGroup(t *testing.T) {
	r, _, _, a := runtimeFixture()
	a.Platform = PlatformGrok
	a.Type = AccountTypeOAuth
	a.Status = StatusActive
	a.Credentials = map[string]any{"access_token": "old", "refresh_token": "old-refresh"}
	a.AttachEgress(r)
	repo := &refreshAPIAccountRepo{account: a}
	changed := int64(8)
	repo.beforeSuccessCAS = func(repo *refreshAPIAccountRepo) {
		copy := *repo.account
		copy.ProxyGroupID = &changed
		repo.account = &copy
	}
	executor := &refreshAPIExecutorStub{needsRefresh: true, credentials: map[string]any{"access_token": "new", "refresh_token": "new-refresh"}}
	result, err := NewOAuthRefreshAPI(repo, nil).RefreshIfNeeded(context.Background(), a, executor, time.Hour)
	require.NoError(t, err)
	require.False(t, result.Refreshed)
	require.Equal(t, "old-refresh", repo.account.GetCredential("refresh_token"))
	require.Equal(t, 1, repo.successCASCalls)
}

func TestProxyGroupMediaAndResourceOutbound(t *testing.T) {
	for _, family := range []string{"audio", "video", "resource poll"} {
		for _, resolved := range []bool{false, true} {
			t.Run(family+map[bool]string{false: "/unresolved", true: "/resolved"}[resolved], func(t *testing.T) {
				r, _, _, binding := runtimeFixture()
				a := openAIActionAccount()
				a.WorkspaceID = 1
				a.ProxyGroupID = binding.ProxyGroupID
				want := ""
				if resolved {
					a.AttachEgress(r)
					var err error
					want, err = a.EgressProxyURL(context.Background())
					require.NoError(t, err)
				}
				base := &egressRecordingUpstream{}
				svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: base}
				var response *http.Response
				var err error
				switch family {
				case "audio":
					response, err = svc.ForwardOpenAIAction(context.Background(), a, "audio.tts", "application/json", []byte(`{"model":"tts-1","input":"hello","voice":"alloy"}`), "")
				case "video":
					response, err = svc.ForwardOpenAIAction(context.Background(), a, "video.generate", "application/json", []byte(`{"model":"sora-2","prompt":"ocean"}`), "")
				case "resource poll":
					response, err = svc.openAIResourceStream(context.Background(), a, "/batches/fixture")
				}
				if !resolved {
					require.ErrorIs(t, err, ErrProxyGroupUnavailable)
					require.Empty(t, base.urls)
				} else {
					require.NoError(t, err)
					require.Equal(t, []string{want}, base.urls)
					_ = response.Body.Close()
				}
			})
		}
	}
}

func TestProxyGroupSavedCatalogDoesNotResolve(t *testing.T) {
	r, g, cache, a := runtimeFixture()
	g.group.Status = "inactive"
	cache.fail = errors.New("cache offline")
	a.AttachEgress(r)
	a.Platform = PlatformOpenAI
	a.Type = AccountTypeAPIKey
	a.Credentials = map[string]any{"model_mapping": map[string]any{"alias": "native"}}
	catalog := projectAccountModelCatalog(a, &UpstreamModelCatalog{Models: []string{"native"}})
	a.Extra = map[string]any{UpstreamModelCatalogExtraKey: catalog}
	require.NotNil(t, a.EffectiveUpstreamModelCatalog())
	require.Equal(t, "native", a.GetMappedModel("alias"))
	repository := &catalogGenerationFixture{generation: 1, accounts: []Account{*a}}
	models, err := catalogFixtureGateway(repository).GetAvailableModelsChecked(context.Background(), nil, PlatformOpenAI)
	require.NoError(t, err)
	require.Equal(t, []string{"alias"}, models)
	require.Zero(t, g.reads)
	require.Zero(t, cache.calls)
}

func TestProxyGroupJobPinDoesNotChangeSubmissionIdentity(t *testing.T) {
	job := &UpstreamJob{Platform: PlatformOpenAI, Family: "video", AccountID: 91, UserID: 1, APIKeyID: 2, Billing: UpstreamJobBilling{Model: "fixture"}}
	before, err := UpstreamJobFingerprint(job, []byte(`{"model":"fixture"}`))
	require.NoError(t, err)
	job.Billing.Egress = &ProxyGroupJobEgress{ProxyGroupID: 7, ProxyID: 11, Source: EgressSourceGroupBinding}
	after, err := UpstreamJobFingerprint(job, []byte(`{"model":"fixture"}`))
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestAntigravityAccessExpiryDoesNotClaimRefreshRevocation(t *testing.T) {
	for _, message := range []string{"HTTP 401: UNAUTHENTICATED", "request timeout", "connection refused"} {
		info := buildAntigravityDegradedUsage(errors.New(message))
		require.False(t, info.NeedsReauth)
	}
	require.True(t, buildAntigravityDegradedUsage(errors.New("refresh rejected: invalid_grant")).NeedsReauth)
	require.False(t, buildAntigravityDegradedUsage(errors.New("HTTP 401"), &Account{Credentials: map[string]any{"refresh_token": "renewable"}}).NeedsReauth)
	require.True(t, buildAntigravityDegradedUsage(errors.New("HTTP 401"), &Account{}).NeedsReauth)
	info := buildAntigravityDegradedUsage(errors.New("HTTP 401"))
	enrichUsageWithAccountError(info, &Account{Status: StatusError, ErrorMessage: "HTTP 403 validation_required"})
	require.True(t, info.NeedsVerify)
	require.False(t, info.NeedsReauth)
}

func TestProxyGroupCredentialDraftTransactionRollbackAndRetry(t *testing.T) {
	for _, failure := range []string{"cache failure", "member revoked", "different winner", "outer rollback"} {
		t.Run(failure, func(t *testing.T) {
			pending, client := newAuthPendingIdentityServiceTestClient(t)
			r, g, cache, _ := runtimeFixture()
			pending.SetEgressRuntime(r)
			ctx := context.Background()
			group, err := client.ProxyGroup.Create().SetName("draft-fixture").Save(ctx)
			require.NoError(t, err)
			g.group.ID = group.ID
			for i := range g.group.Members {
				g.group.Members[i].GroupID = group.ID
			}
			target, err := r.ResolveForNewSession(ctx, group.ID, "server-seed")
			require.NoError(t, err)
			selection := &OAuthEgressSelection{GroupID: group.ID, WorkspaceID: 1, ProxyID: target.ProxyID, URL: target.URL, Source: string(target.Source)}
			draft, err := pending.IssueAccountCredentialDraft(ctx, "browser", PlatformOpenAI, AccountTypeOAuth, map[string]any{"refresh_token": "rotated-fixture"}, selection)
			require.NoError(t, err)
			inject := true
			create := func(txCtx context.Context, a *Account) (int64, error) {
				row, err := dbent.TxFromContext(txCtx).Account.Create().SetName("group-draft-account").SetPlatform(a.Platform).SetType(a.Type).SetCredentials(a.Credentials).SetNillableProxyGroupID(a.ProxyGroupID).Save(txCtx)
				if err != nil {
					return 0, err
				}
				if inject {
					switch failure {
					case "cache failure":
						cache.fail = errors.New("cache unavailable")
					case "member revoked":
						g.group.Status = "inactive"
					case "different winner":
						cache.bindings[[2]int64{group.ID, row.ID}] = 999
					}
				}
				return row.ID, nil
			}
			confirm := func(ctx context.Context) (int64, error) {
				return pending.ConfirmAccountCredentialDraft(ctx, draft.Reference, "browser", draft.Revision, "reviewed", create)
			}
			if failure == "outer rollback" {
				err = pending.RunAccountWriteTransaction(ctx, func(txCtx context.Context) error {
					_, err := confirm(txCtx)
					require.NoError(t, err)
					return errors.New("later batch item failed")
				})
			} else {
				_, err = confirm(ctx)
			}
			require.Error(t, err)
			require.Zero(t, client.Account.Query().CountX(ctx))
			retryDraft, _, err := pending.ResolveAccountCredentialDraft(ctx, draft.Reference, "browser")
			require.NoError(t, err)
			require.Equal(t, "rotated-fixture", retryDraft.GetCredential("refresh_token"))
			if failure == "outer rollback" {
				require.Empty(t, cache.bindings)
			}
			if failure == "different winner" {
				require.Len(t, cache.bindings, 1)
				for _, winner := range cache.bindings {
					require.Equal(t, int64(999), winner)
				}
			}
			inject = false
			cache.fail = nil
			cache.bindings = map[[2]int64]int64{}
			g.group.Status = StatusActive
			id, err := confirm(ctx)
			require.NoError(t, err)
			require.Positive(t, id)
			replay, err := confirm(ctx)
			require.NoError(t, err)
			require.Equal(t, id, replay)
			require.Equal(t, 1, client.Account.Query().CountX(ctx))
			require.Equal(t, target.ProxyID, cache.bindings[[2]int64{group.ID, id}])
			stored := client.Account.GetX(ctx, id)
			require.Equal(t, group.ID, *stored.ProxyGroupID)
		})
	}
}

func int64Key(group, account int64) [2]int64 { return [2]int64{group, account} }
