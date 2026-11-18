//go:build unit

package service

import (
	"bytes"
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

func specialAuthAccount(platform, mode, base, key string) *Account {
	return &Account{ID: 731, WorkspaceID: 9, Platform: platform, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 3, Credentials: map[string]any{"account_mode": "payg", "auth_mode": mode, "base_url": base, "api_key": key, "model_mapping": map[string]any{"alias": "deployment-one"}}}
}

func TestCopilotNousSavedValidationAndBinding(t *testing.T) {
	for _, platform := range []string{"github_copilot", "nous_research"} {
		mode, key := CopilotAuthFineGrainedPAT, "github_pat_fixture"
		if platform == "nous_research" {
			mode, key = NousAuthInvocationToken, authCoreJWT(fmt.Sprintf(`{"exp":%d,"scope":"inference:invoke"}`, time.Now().Add(time.Hour).Unix()))
		}
		account := specialAuthAccount(platform, mode, "", key)
		metadata := CredentialSecret{Reference: "reference-fixture", Version: 2, AccountID: account.ID, WorkspaceID: account.WorkspaceID}
		inline, secrets := SplitCredentialSecrets(account.Credentials)
		require.NotContains(t, inline, "api_key")
		require.Equal(t, mode, inline["auth_mode"])
		ciphertext, err := EncryptCredentialSecret(credentialTestEncryptor{}, metadata, secrets)
		require.NoError(t, err)
		inline[CredentialSecretReferenceKey], inline[CredentialSecretVersionKey] = metadata.Reference, metadata.Version
		created, err := buildAccountForCreate(&CreateAccountInput{Platform: platform, Type: AccountTypeAPIKey, Credentials: inline}, nil)
		require.NoError(t, err, "save must not require plaintext before repository resolution")
		require.Equal(t, metadata.Reference, created.Credentials[CredentialSecretReferenceKey])
		account.Credentials = maps.Clone(inline)
		_, err = bindCompatibleRequest(context.Background(), account, "chat", "alias")
		require.Error(t, err, "unresolved reference must not become bearer")
		resolved, err := ResolveCredentialSecret(context.Background(), credentialTestEncryptor{}, metadata, ciphertext)
		require.NoError(t, err)
		maps.Copy(account.Credentials, resolved)
		for _, action := range []string{"chat", "messages", "responses", "generate_content"} {
			bound, err := bindCompatibleRequest(context.Background(), account, action, "alias")
			require.NoError(t, err)
			require.Equal(t, APIProtocolChatCompletions, bound.GetAPIProtocol())
			require.NotContains(t, bound.GetOpenAIBaseURL(), "api.openai.com")
			require.Equal(t, "payg", bound.GetCredential("account_mode"))
		}
		for _, patch := range []map[string]any{{"refresh_token": ""}, {"base_url": "https://api.openai.com"}, {"auth_mode": "oauth"}, {"account_mode": "coding"}, {"api_base_urls": map[string]any{"chat_completions": "https://evil.test"}}, {"api_base_urls": map[string]any{"chat_completions": map[string]any{}}}} {
			candidate := *account
			candidate.Credentials = maps.Clone(account.Credentials)
			maps.Copy(candidate.Credentials, patch)
			require.Error(t, validateExpandedPlatformAccount(&candidate))
			repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{account}}
			_, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{account.ID}, Credentials: patch})
			require.Error(t, err)
			require.Zero(t, repo.bulkUpdateCalls)
		}
		metadata.AccountID++
		_, err = ResolveCredentialSecret(context.Background(), credentialTestEncryptor{}, metadata, ciphertext)
		require.ErrorIs(t, err, ErrCredentialSecretInvalid)
	}
	proxy := specialAuthAccount("nous_research", NousAuthOfficialProxy, "http://127.0.0.1:8645/v1", "")
	_, err := buildAccountForCreate(&CreateAccountInput{Platform: proxy.Platform, Type: proxy.Type, Credentials: proxy.Credentials}, nil)
	require.NoError(t, err)
	bound, err := bindCompatibleRequest(context.Background(), proxy, "messages", "alias")
	require.NoError(t, err)
	key, _, err := (&OpenAIGatewayService{}).GetAccessToken(context.Background(), bound)
	require.NoError(t, err)
	require.Empty(t, key)
}

type specialAuthRecordingUpstream struct {
	request     *http.Request
	calls       int
	proxy       string
	id          int64
	concurrency int
	status      int
}

func (upstream *specialAuthRecordingUpstream) Do(request *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	upstream.calls++
	upstream.request = request
	upstream.proxy = proxy
	upstream.id = id
	upstream.concurrency = concurrency
	return &http.Response{StatusCode: upstream.status, Body: http.NoBody, Header: http.Header{}}, nil
}
func (upstream *specialAuthRecordingUpstream) DoWithTLS(request *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return upstream.Do(request, proxy, id, concurrency)
}

func TestCopilotNousFinalSendAndDiscovery(t *testing.T) {
	account := specialAuthAccount("github_copilot", "", "", "github_pat_fixture")
	var validated []string
	request, err := buildCompatibleModelsRequest(context.Background(), account, func(raw string) (string, error) { validated = append(validated, raw); return raw, nil })
	require.NoError(t, err)
	require.Equal(t, []string{"https://api.githubcopilot.com/v1", "https://api.githubcopilot.com/models"}, validated)
	require.Equal(t, "https://api.githubcopilot.com/models", request.URL.String())
	require.Equal(t, CopilotModelsAPIVersion, request.Header.Get("X-GitHub-Api-Version"))
	transport := &specialAuthRecordingUpstream{status: 200}
	for _, catalog := range []bool{true, false} {
		if !catalog {
			request, err = http.NewRequest(http.MethodPost, "https://api.githubcopilot.com/v1/chat/completions", nil)
			require.NoError(t, err)
		}
		request.Header["authorization"] = []string{"Bearer wrong"}
		request.Header.Set("Copilot-Integration-Id", "agentic-workflows")
		request.Header.Set("X-GitHub-Api-Version", "wrong")
		request.Header.Set("X-Api-Key", "wrong")
		request.Header.Set("X-Request-Id", "allowed")
		_, err = accountHTTPUpstream(transport, account).Do(request, "http://egress.test:3128", account.ID, 3)
		require.NoError(t, err)
		require.True(t, HTTPUpstreamRedirectsDisabled(transport.request.Context()))
		require.Equal(t, "http://egress.test:3128", transport.proxy)
		require.Equal(t, account.ID, transport.id)
		require.Equal(t, 3, transport.concurrency)
		require.Equal(t, "Bearer github_pat_fixture", transport.request.Header.Get("Authorization"))
		require.Equal(t, "omnihive", transport.request.Header.Get("Copilot-Integration-Id"))
		require.Empty(t, transport.request.Header.Get("X-Api-Key"))
		require.Equal(t, "allowed", transport.request.Header.Get("X-Request-Id"))
		if !catalog {
			require.Empty(t, transport.request.Header.Get("X-GitHub-Api-Version"))
		}
	}
	for _, target := range []struct{ method, url string }{{"GET", "https://api.githubcopilot.com/v1/models"}, {"POST", "https://api.openai.com/v1/chat/completions"}, {"GET", "https://api.githubcopilot.com/v1/chat/completions"}} {
		request, _ := http.NewRequest(target.method, target.url, nil)
		before := transport.calls
		_, err := accountHTTPUpstream(transport, account).Do(request, "", 731, 3)
		require.Error(t, err)
		require.Equal(t, before, transport.calls)
	}
	for _, status := range []int{401, 403, 429} {
		transport.status = status
		response, err := accountHTTPUpstream(transport, account).Do(request, "", 731, 3)
		require.NoError(t, err)
		require.Equal(t, status, response.StatusCode)
	}
	transport.status = 307
	_, err = accountHTTPUpstream(transport, account).Do(request, "", 731, 3)
	require.ErrorContains(t, err, "307")
	nous := specialAuthAccount("nous_research", NousAuthInvocationToken, "", authCoreJWT(fmt.Sprintf(`{"exp":%d,"scope":"inference:invoke"}`, time.Now().Add(time.Hour).Unix())))
	bound, err := bindCompatibleRequest(context.Background(), nous, "chat", "alias")
	require.NoError(t, err)
	bound.Credentials["api_key"] = authCoreJWT(`{"exp":1,"scope":"inference:invoke"}`)
	request, _ = http.NewRequest("POST", "https://inference-api.nousresearch.com/v1/chat/completions", nil)
	before := transport.calls
	_, err = accountHTTPUpstream(transport, bound).Do(request, "", 731, 3)
	require.ErrorIs(t, err, ErrCopilotNousExpired)
	require.Equal(t, before, transport.calls)
}

func TestCopilotNousProxyCatalogAndProbe(t *testing.T) {
	for _, key := range []string{"", "frontdoor-fixture"} {
		var paths []string
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			paths = append(paths, request.Method+" "+request.URL.Path)
			if key == "" {
				require.Empty(t, request.Header.Get("Authorization"))
			} else {
				require.Equal(t, "Bearer "+key, request.Header.Get("Authorization"))
			}
			if request.Method == "GET" {
				writer.Header().Set("Content-Type", "application/json")
				fmt.Fprint(writer, `{"data":[{"id":"deployment-one"}]}`)
			} else {
				writer.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(writer, compatibleSSE(APIProtocolChatCompletions))
			}
		}))
		account := specialAuthAccount("nous_research", NousAuthOfficialProxy, server.URL+"/prefix", key)
		svc := &AccountTestService{cfg: rawChatCompletionsTestConfig(), httpUpstream: &compatibleLoopback{client: server.Client()}}
		request, err := svc.buildUpstreamModelsRequest(context.Background(), account)
		require.NoError(t, err)
		response, err := svc.doUpstreamModelsRequest(request, "", account)
		require.NoError(t, err)
		_, err = io.ReadAll(response.Body)
		require.NoError(t, err)
		response.Body.Close()
		probe := &accountProbeContext{ctx: context.Background(), emit: func(TestEvent) {}}
		require.NoError(t, svc.testCompatibleAccountConnection(probe, account, "alias", "hello"))
		require.Equal(t, []string{"GET /prefix/models", "POST /prefix/chat/completions"}, paths)
		server.Close()
	}
}

func TestCopilotNousDescriptorBridges(t *testing.T) {
	for _, platform := range []string{"github_copilot", "nous_research"} {
		provider, ok := domain.LookupCompatibleProvider(platform)
		require.True(t, ok)
		require.Len(t, provider.Modes, 1)
		require.Equal(t, "payg", provider.DefaultMode)
		for _, action := range []string{"messages", "responses", "generate_content"} {
			endpoint, _, ok := provider.Modes[0].ResolveAction(action, "")
			require.True(t, ok)
			require.Equal(t, APIProtocolChatCompletions, endpoint.Protocol)
		}
		descriptor, ok := domain.LookupPlatform(platform)
		require.True(t, ok)
		require.False(t, descriptor.NativeMultiProtocolResponses)
		require.Equal(t, domain.EndpointBridge, descriptor.MessagesPOST)
		require.Equal(t, domain.EndpointBridge, descriptor.ResponsesPOST)
	}
}

func TestCopilotNousAuthModeTransitionAdminBoundary(t *testing.T) {
	for _, referenced := range []bool{false, true} {
		for _, bulk := range []bool{false, true} {
			for _, replacement := range []any{nil, "********", "old-invocation-fixture", "", "new-frontdoor-fixture"} {
				account := specialAuthAccount("nous_research", NousAuthInvocationToken, "", "old-invocation-fixture")
				if referenced {
					account.Credentials[CredentialSecretReferenceKey] = "bound-ref"
					account.Credentials[CredentialSecretVersionKey] = int64(3)
				}
				patch := map[string]any{"auth_mode": NousAuthOfficialProxy, "base_url": "https://operator.test/v1"}
				if replacement != nil {
					patch["api_key"] = replacement
				}
				repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{account}, getByIDAccounts: map[int64]*Account{account.ID: account}}
				svc := &adminServiceImpl{accountRepo: repo}
				var err error
				if bulk {
					_, err = svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{account.ID}, Credentials: patch})
				} else {
					_, err = svc.UpdateAccount(context.Background(), account.ID, &UpdateAccountInput{Credentials: patch})
				}
				if referenced && bulk {
					requireApplicationErrorReason(t, err, "SPECIAL_AUTH_REFERENCED_BULK_UNSUPPORTED")
					require.Zero(t, repo.bulkUpdateCalls)
					require.Empty(t, repo.updatedAccounts)
				} else if replacement == "" || replacement == "new-frontdoor-fixture" {
					require.NoError(t, err)
				} else {
					requireApplicationErrorReason(t, err, "SPECIAL_AUTH_SECRET_REPLACEMENT_REQUIRED")
					require.Empty(t, repo.updatedAccounts)
					require.Zero(t, repo.bulkUpdateCalls)
					require.Equal(t, NousAuthInvocationToken, account.GetCredential("auth_mode"))
				}
			}
		}
	}
	account := specialAuthAccount("nous_research", NousAuthInvocationToken, "", "old-invocation-fixture")
	account.Credentials[CredentialSecretReferenceKey] = "bound-ref"
	account.Credentials[CredentialSecretVersionKey] = int64(3)
	patch := maps.Clone(account.Credentials)
	delete(patch, "api_key")
	repo := &accountRepoStubForBulkUpdate{getByIDAccounts: map[int64]*Account{account.ID: account}}
	saved, err := (&adminServiceImpl{accountRepo: repo}).UpdateAccount(context.Background(), account.ID, &UpdateAccountInput{Credentials: patch})
	require.NoError(t, err)
	require.Equal(t, "old-invocation-fixture", saved.GetCredential("api_key"))
	require.Equal(t, "bound-ref", saved.Credentials[CredentialSecretReferenceKey])
}

func TestCopilotNousGatewayCancellation(t *testing.T) {
	t.Run("bounded request cancellation", func(t *testing.T) {
		arrived := make(chan struct{})
		cancelled := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			close(arrived)
			select {
			case <-r.Context().Done():
				close(cancelled)
			case <-time.After(4 * time.Second):
			}
		}))
		defer server.Close()
		gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: &compatibleLoopback{client: server.Client()}}
		ctx, cancel := context.WithCancel(context.WithValue(context.Background(), boundedStreamContextKey{}, true))
		defer cancel()
		body := []byte(`{"model":"alias","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body)).WithContext(ctx)
		done := make(chan error, 1)
		go func() {
			_, err := gateway.ForwardAsChatCompletions(ctx, c, specialAuthAccount("nous_research", NousAuthOfficialProxy, server.URL+"/v1", ""), body, "", "")
			done <- err
		}()
		select {
		case <-arrived:
		case <-time.After(2 * time.Second):
			t.Fatal("request did not reach loopback")
		}
		cancel()
		select {
		case err := <-done:
			require.Error(t, err)
		case <-time.After(2 * time.Second):
			t.Fatal("cancelled request did not return")
		}
		select {
		case <-cancelled:
		case <-time.After(2 * time.Second):
			t.Fatal("upstream context did not cancel")
		}
	})
}
func TestCopilotNousAdminCreateAccount(t *testing.T) {
	for _, platform := range []string{"github_copilot", "nous_research"} {
		credentials := map[string]any{"api_key": "github_pat_fixture"}
		if platform == "nous_research" {
			credentials = map[string]any{"auth_mode": NousAuthOfficialProxy, "base_url": "https://operator.test/v1"}
		}
		repo := &accountRepoStubForBulkUpdate{createID: 731}
		account, err := (&adminServiceImpl{accountRepo: repo}).CreateAccount(context.Background(), &CreateAccountInput{Name: "fixture", Platform: platform, Type: AccountTypeAPIKey, Credentials: credentials, SkipDefaultGroupBind: true})
		require.NoError(t, err)
		require.Equal(t, platform, account.Platform)
		require.NotNil(t, repo.createAccount)
	}
}

func TestCopilotNousReferencedBulkAllTargetsPrevalidated(t *testing.T) {
	inline := specialAuthAccount("nous_research", NousAuthInvocationToken, "", "old-jwt")
	referenced := specialAuthAccount("nous_research", NousAuthInvocationToken, "", "old-jwt")
	referenced.ID = 732
	referenced.Credentials[CredentialSecretReferenceKey] = "bound-reference"
	repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{inline, referenced}}
	_, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{inline.ID, referenced.ID}, Credentials: map[string]any{"auth_mode": NousAuthOfficialProxy, "base_url": "https://new-operator.invalid/v1", "api_key": ""}})
	requireApplicationErrorReason(t, err, "SPECIAL_AUTH_REFERENCED_BULK_UNSUPPORTED")
	require.Zero(t, repo.bulkUpdateCalls)
	require.Empty(t, repo.bindGroupsCalls)
	require.Equal(t, NousAuthInvocationToken, inline.GetCredential("auth_mode"))
	require.Equal(t, NousAuthInvocationToken, referenced.GetCredential("auth_mode"))
	_, err = (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{inline.ID, referenced.ID}, Credentials: map[string]any{"auth_mode": NousAuthInvocationToken, "api_protocol": "adaptive"}})
	require.NoError(t, err)
	require.Equal(t, 1, repo.bulkUpdateCalls)
}
