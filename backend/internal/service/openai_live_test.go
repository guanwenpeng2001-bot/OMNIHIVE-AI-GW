package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type liveHTTPUpstreamStub struct {
	request *http.Request
	body    []byte
}

type liveAttestationStub struct {
	header string
	err    error
}

func (s liveAttestationStub) Check(context.Context) error {
	return s.err
}

func (s liveAttestationStub) Generate(context.Context) (string, error) {
	return s.header, s.err
}

func (s *liveHTTPUpstreamStub) Do(
	request *http.Request,
	_ string,
	_ int64,
	_ int,
) (*http.Response, error) {
	s.request = request
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	s.body = body
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Location": {"/backend-api/codex/call_test"},
		},
		Body: io.NopCloser(strings.NewReader("v=0\r\n")),
	}, nil
}

func (s *liveHTTPUpstreamStub) DoWithTLS(
	request *http.Request,
	proxyURL string,
	accountID int64,
	accountConcurrency int,
	_ *tlsfingerprint.Profile,
) (*http.Response, error) {
	return s.Do(request, proxyURL, accountID, accountConcurrency)
}

func TestLiveCapabilityOnlyAllowsOpenAIOAuth(t *testing.T) {
	require.True(t, (&Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}).SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityLive))
	require.False(t, (&Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}).SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityLive))
	require.False(t, (&Account{Platform: PlatformGrok, Type: AccountTypeOAuth}).SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityLive))
	require.False(t, (&Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			openAIAuthModeCredentialKey: OpenAIAuthModePersonalAccessToken,
		},
	}).SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityLive))
	require.False(t, (&Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			openAIAuthModeCredentialKey: OpenAIAuthModeAgentIdentity,
		},
	}).SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityLive))
}

func TestValidateLiveCallRequestDoesNotRequireDelegation(t *testing.T) {
	request := &LiveCallRequest{
		SDP:     "v=0\r\n",
		Session: json.RawMessage(`{"model":"gpt-live-test","instructions":"hello"}`),
	}
	require.NoError(t, ValidateLiveCallRequest(request))
	require.NotContains(t, string(request.Session), "delegation")
}

func TestCreateUpstreamLiveCallPreservesSession(t *testing.T) {
	upstream := &liveHTTPUpstreamStub{}
	service := &OpenAIGatewayService{
		cfg:          &config.Config{},
		httpUpstream: upstream,
	}
	account := &Account{
		ID:          7,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 2,
		Credentials: map[string]any{
			"access_token":       "test-access-token",
			"chatgpt_account_id": "acct_test",
		},
	}
	session := json.RawMessage(`{
		"model":"gpt-live-test",
		"delegation":{"type":"client"},
		"custom":{"keep":true}
	}`)

	created, err := service.createUpstreamLiveCall(context.Background(), account, &LiveCallRequest{
		SDP:     "v=offer\r\n",
		Session: session,
	}, `{"v":1,"s":0,"t":"v1.test"}`)
	require.NoError(t, err)
	require.Equal(t, "call_test", created.CallID)
	require.Equal(t, []byte("v=0\r\n"), created.SDP)

	var forwarded struct {
		SDP     string          `json:"sdp"`
		Session json.RawMessage `json:"session"`
	}
	require.NoError(t, json.Unmarshal(upstream.body, &forwarded))
	require.Equal(t, "v=offer\r\n", forwarded.SDP)
	require.JSONEq(t, string(session), string(forwarded.Session))
	require.Equal(t, "Bearer test-access-token", upstream.request.Header.Get("Authorization"))
	require.Equal(t, "acct_test", upstream.request.Header.Get("Chatgpt-Account-Id"))
	require.Equal(t, "quicksilver=v2", upstream.request.Header.Get("OpenAI-Alpha"))
	require.Equal(t, `{"v":1,"s":0,"t":"v1.test"}`, upstream.request.Header.Get(liveAttestationHeader))
	require.NotEmpty(t, upstream.request.Header.Get("Session-Id"))
	require.NotEmpty(t, upstream.request.Header.Get("Thread-Id"))
	require.Empty(t, upstream.request.Header.Get("OpenAI-Beta"))
	require.Equal(t, HTTPUpstreamProfileOpenAI, HTTPUpstreamProfileFromContext(upstream.request.Context()))
	require.True(t, HTTPUpstreamRedirectsDisabled(upstream.request.Context()))
}

func TestLiveAttestationCipherRoundTripAndRejectsOtherInstanceKey(t *testing.T) {
	first := newLiveAttestationCipher(&config.Config{
		JWT: config.JWTConfig{Secret: "first-live-secret"},
	})
	second := newLiveAttestationCipher(&config.Config{
		JWT: config.JWTConfig{Secret: "second-live-secret"},
	})
	require.NotNil(t, first)
	require.NotNil(t, second)

	ciphertext, err := first.Encrypt(`{"v":1,"s":0,"t":"v1.opaque"}`)
	require.NoError(t, err)
	require.NotContains(t, ciphertext, "opaque")

	plaintext, err := first.Decrypt(ciphertext)
	require.NoError(t, err)
	require.Equal(t, `{"v":1,"s":0,"t":"v1.opaque"}`, plaintext)

	_, err = second.Decrypt(ciphertext)
	require.Error(t, err)
}

func TestPrepareLiveAttestationEncryptsHeaderAndReturnsExplicitProviderError(t *testing.T) {
	cipher := newLiveAttestationCipher(&config.Config{
		JWT: config.JWTConfig{Secret: "live-attestation-test-secret"},
	})
	service := &OpenAIGatewayService{
		liveAttestation:       liveAttestationStub{header: `{"v":1,"s":0,"t":"v1.test"}`},
		liveAttestationCipher: cipher,
	}
	header, ciphertext, err := service.prepareLiveAttestation(context.Background())
	require.NoError(t, err)
	require.Equal(t, `{"v":1,"s":0,"t":"v1.test"}`, header)
	require.NotContains(t, ciphertext, "v1.test")
	decrypted, err := cipher.Decrypt(ciphertext)
	require.NoError(t, err)
	require.Equal(t, header, decrypted)

	service.liveAttestation = liveAttestationStub{err: errors.New("macOS app missing")}
	_, _, err = service.prepareLiveAttestation(context.Background())
	var unavailable *LiveAttestationUnavailableError
	require.ErrorAs(t, err, &unavailable)
	require.Contains(t, unavailable.Error(), "macOS app missing")
}

func TestLiveMaxSessionDurationDefaultsAndOverrides(t *testing.T) {
	require.Equal(t, defaultLiveMaxSessionDuration, (&OpenAIGatewayService{}).liveMaxSessionDuration())
	require.Equal(
		t,
		90*time.Second,
		(&OpenAIGatewayService{cfg: &config.Config{
			Gateway: config.GatewayConfig{
				Live: config.GatewayLiveConfig{MaxSessionDurationSeconds: 90},
			},
		}}).liveMaxSessionDuration(),
	)
}

func TestLiveSidebandNormalCloseEndsCall(t *testing.T) {
	normalClose := coderws.CloseError{Code: coderws.StatusNormalClosure}
	require.ErrorIs(t, liveSidebandReadError(normalClose), ErrLiveCallNotFound)

	abnormalClose := coderws.CloseError{Code: coderws.StatusInternalError}
	require.Equal(t, abnormalClose, liveSidebandReadError(abnormalClose))
}

func TestLiveCreateFailoverUsesExistingOpenAIPolicy(t *testing.T) {
	service := &OpenAIGatewayService{}
	account := newOpenAIUpstreamErrorTestAccount()
	require.False(t, service.shouldFailoverLiveCreateError(account, &UpstreamFailoverError{
		StatusCode:   http.StatusBadRequest,
		ResponseBody: []byte(`{"error":{"message":"invalid session"}}`),
	}))
	require.True(t, service.shouldFailoverLiveCreateError(account, &UpstreamFailoverError{
		StatusCode: http.StatusForbidden,
	}))
	require.True(t, service.shouldFailoverLiveCreateError(account, &UpstreamFailoverError{
		StatusCode: http.StatusBadGateway,
	}))
	require.True(t, service.shouldFailoverLiveCreateError(account, errors.New("transport failed")))
}

func TestLiveCallIDFromLocation(t *testing.T) {
	callID, err := liveCallIDFromLocation("https://chatgpt.com/backend-api/codex/call_123?intent=quicksilver")
	require.NoError(t, err)
	require.Equal(t, "call_123", callID)

	callID, err = liveCallIDFromLocation("/backend-api/codex/call_456")
	require.NoError(t, err)
	require.Equal(t, "call_456", callID)
}

func TestRequestTypeLive(t *testing.T) {
	require.True(t, RequestTypeLive.IsValid())
	require.Equal(t, "live", RequestTypeLive.String())
	parsed, err := ParseUsageRequestType("live")
	require.NoError(t, err)
	require.Equal(t, RequestTypeLive, parsed)
}

func TestLiveBillingPolicyRequiresExplicitFree(test *testing.T) {
	for _, policy := range []string{"", "paid", "duration", "FREE", " free ", "free"} {
		test.Run(policy, func(test *testing.T) {
			cfg := &config.Config{}
			cfg.Gateway.Live.BillingPolicy = policy
			gateway := &OpenAIGatewayService{cfg: cfg}
			err := gateway.CheckLiveBillingPolicy()
			if policy == "free" {
				require.NoError(test, err)
				_, err = gateway.CreateLiveCall(context.Background(), nil, LiveCallIdentity{}, 1)
				require.EqualError(test, err, "sdp is required")
			} else {
				require.ErrorIs(test, err, ErrLiveBillingUnsupported)
				// No dependencies: rejection must happen before validation or upstream work.
				_, err = gateway.CreateLiveCall(context.Background(), nil, LiveCallIdentity{}, 1)
				require.ErrorIs(test, err, ErrLiveBillingUnsupported)
				require.Contains(test, err.Error(), "free Live only")
				require.Contains(test, err.Error(), "paid Live is not supported")
				require.Contains(test, err.Error(), "set gateway.live.billing_policy explicitly to free")
				if policy == "" {
					require.Contains(test, err.Error(), "the Live feature is disabled")
					require.Contains(test, err.Error(), "not configured")
				} else {
					require.Contains(test, err.Error(), "unsupported gateway.live.billing_policy")
					require.Contains(test, err.Error(), "accepted values: empty to disable, free to enable")
				}
			}
		})
	}
	var gateway *OpenAIGatewayService
	require.ErrorIs(test, gateway.CheckLiveBillingPolicy(), ErrLiveBillingUnsupported)
}

type liveFreePolicyScheduler struct {
	OpenAIAccountScheduler
	account *Account
}

func (scheduler *liveFreePolicyScheduler) Select(context.Context, OpenAIAccountScheduleRequest) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	return &AccountSelectionResult{Account: scheduler.account, Acquired: true, ReleaseFunc: func() {}}, OpenAIAccountScheduleDecision{}, nil
}

type liveFreePolicyStore struct {
	liveTestStore
}

func (store *liveFreePolicyStore) GetSessionAccountID(context.Context, int64, string) (int64, error) {
	return 0, nil
}

type liveFreePolicyBillingRepo struct {
	UsageBillingRepository
	charges int
}

func (repo *liveFreePolicyBillingRepo) Apply(context.Context, *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	repo.charges++
	return &UsageBillingApplyResult{}, nil
}

func TestCreateLiveCallExplicitFreeDoesNotCharge(test *testing.T) {
	openAIAdvancedSchedulerSettingCache.Store(&cachedOpenAIAdvancedSchedulerSetting{
		enabled: true, expiresAt: time.Now().Add(time.Hour).UnixNano(),
	})
	test.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	configuration := &config.Config{JWT: config.JWTConfig{Secret: "live-free-test-secret"}}
	configuration.Gateway.Live.BillingPolicy = "free"
	account := &Account{
		ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 2,
		Credentials: map[string]any{"access_token": "test-token", "chatgpt_account_id": "test-account"},
	}
	store := &liveFreePolicyStore{}
	usageRepo := &liveTestUsageRepo{}
	billingRepo := &liveFreePolicyBillingRepo{}
	upstream := &liveHTTPUpstreamStub{}
	sideband := newLiveTestFrameConn()
	sideband.reads <- liveTestFrame{messageType: coderws.MessageText, payload: []byte(`{"type":"session.closed"}`)}
	gateway := &OpenAIGatewayService{
		cfg: configuration, cache: store, usageLogRepo: usageRepo, usageBillingRepo: billingRepo,
		accountRepo: &liveTestAccountRepo{account: account}, httpUpstream: upstream,
		openaiScheduler:           &liveFreePolicyScheduler{account: account},
		concurrencyService:        &ConcurrencyService{cache: &liveTestConcurrencyCache{}},
		liveAttestation:           liveAttestationStub{header: "test-attestation"},
		liveAttestationCipher:     newLiveAttestationCipher(configuration),
		openaiWSPassthroughDialer: &liveTestDialer{conn: sideband},
	}
	created, err := gateway.CreateLiveCall(context.Background(), &LiveCallRequest{
		SDP: "v=offer\r\n", Session: json.RawMessage(`{"model":"gpt-live-test"}`),
	}, LiveCallIdentity{UserID: 12, APIKeyID: 34}, 1)
	require.NoError(test, err)
	require.Equal(test, "call_test", created.CallID)
	record, err := store.GetLiveCall(context.Background(), hashLiveCallID(created.CallID))
	require.NoError(test, err)
	require.Equal(test, LiveFreeReason, record.FreeReason)
	require.Eventually(test, func() bool {
		usageRepo.mu.Lock()
		defer usageRepo.mu.Unlock()
		return len(usageRepo.logs) == 1
	}, 3*time.Second, 10*time.Millisecond)
	usageRepo.mu.Lock()
	defer usageRepo.mu.Unlock()
	require.Equal(test, PricingStateFree, usageRepo.logs[0].PricingState)
	require.Zero(test, usageRepo.logs[0].TotalCost)
	require.Zero(test, usageRepo.logs[0].ActualCost)
	require.Zero(test, billingRepo.charges)
}

func TestFinalizeLiveCallExplicitFreeReasonSurvivesPolicyChange(t *testing.T) {
	record := &LiveCallRecord{CallHash: "free-call", FreeReason: LiveFreeReason, CreatedAt: time.Now().Add(-time.Hour)}
	store := &liveTestStore{}
	require.NoError(t, store.SaveLiveCall(context.Background(), record, time.Hour))
	repo := &liveTestUsageRepo{}
	gateway := &OpenAIGatewayService{cache: store, usageLogRepo: repo, cfg: &config.Config{}}
	gateway.finalizeLiveCall(record)
	require.Len(t, repo.logs, 1)
	require.Equal(t, PricingStateFree, repo.logs[0].PricingState)
	require.Zero(t, repo.logs[0].TotalCost)
	require.Zero(t, repo.logs[0].ActualCost)
}
