package routes

import (
	"context"
	"crypto/tls"
	"errors"
	ws "github.com/coder/websocket"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// These fixtures stop at durable session admission, before any socket is opened.
// All requests still traverse the registered production middleware and handlers.
type realtimeRouteAccounts struct {
	service.AccountRepository
	account service.Account
}

func (repository *realtimeRouteAccounts) ListSchedulableByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	if platform != repository.account.Platform {
		return nil, nil
	}
	return []service.Account{repository.account}, nil
}
func (repository *realtimeRouteAccounts) ListSchedulableByGroupIDAndPlatform(ctx context.Context, _ int64, platform string) ([]service.Account, error) {
	return repository.ListSchedulableByPlatform(ctx, platform)
}
func (repository *realtimeRouteAccounts) GetByID(context.Context, int64) (*service.Account, error) {
	copy := repository.account
	return &copy, nil
}

type realtimeRouteConcurrency struct{ service.ConcurrencyCache }

func (*realtimeRouteConcurrency) AcquireUserSlot(context.Context, int64, int, string) (bool, error) {
	return true, nil
}
func (*realtimeRouteConcurrency) ReleaseUserSlot(context.Context, int64, string) error { return nil }
func (*realtimeRouteConcurrency) AcquireAccountSlot(context.Context, int64, int, string) (bool, error) {
	return true, nil
}
func (*realtimeRouteConcurrency) ReleaseAccountSlot(context.Context, int64, string) error { return nil }

type realtimeRouteJobs struct {
	service.UpstreamJobRepository
	session        *service.UpstreamJob
	receipt        chan *service.UpstreamJob
	finished       chan struct{}
	storedReceipt  *service.UpstreamJob
	charges        int
	chargedCommand *service.UsageBillingCommand
	receiptError   error
}

func (repository *realtimeRouteJobs) CreateRealtimeSession(_ context.Context, job *service.UpstreamJob) (*service.UpstreamJob, error) {
	repository.session = job
	if repository.receipt != nil {
		return job, nil
	}
	return nil, errors.New("fixture stops before network")
}

type realtimeRouteChannels struct {
	service.ChannelRepository
	platform, mappingPlatform, requested, mapped string
}

func (repository *realtimeRouteChannels) ListAll(context.Context) ([]service.Channel, error) {
	return []service.Channel{{ID: 1, Status: service.StatusActive, GroupIDs: []int64{3}, ModelMapping: map[string]map[string]string{repository.mappingPlatform: {repository.requested: repository.mapped}}}}, nil
}
func (repository *realtimeRouteChannels) GetGroupPlatforms(context.Context, []int64) (map[int64]string, error) {
	return map[int64]string{3: repository.platform}, nil
}

func realtimeProductionRouter(test *testing.T, account service.Account, groupPlatform, publicModel, targetModel, channelModel string, jobs service.UpstreamJobRepository, upstream ...service.HTTPUpstream) *gin.Engine {
	test.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{AllowInsecureHTTP: true}}, RunMode: config.RunModeSimple, Gateway: config.GatewayConfig{ConcurrencySlotTTLMinutes: 3, MaxBodySize: 1 << 20, TextMaxBodySize: 1 << 20}}
	repository := &realtimeRouteAccounts{account: account}
	var channels *service.ChannelService
	if channelModel != "" {
		channels = service.NewChannelService(&realtimeRouteChannels{platform: groupPlatform, mappingPlatform: account.Platform, requested: publicModel, mapped: channelModel}, nil, nil, nil, nil)
		groupID := int64(3)
		mapping, _ := channels.ResolveChannelMappingAndRestrict(service.WithResolvedTargetPlatform(context.Background(), account.Platform), &groupID, publicModel)
		require.True(test, mapping.Mapped, "fixture must exercise a real channel mapping")
		require.Equal(test, channelModel, mapping.MappedModel)
	}
	var transport service.HTTPUpstream
	if len(upstream) > 0 {
		transport = upstream[0]
	}
	gateway := service.ProvideOpenAIGatewayService(repository, nil, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, nil, transport, nil, nil, nil, nil, channels, nil, nil, nil, service.NewUsageSettlementService(&realtimeRouteTPM{}, nil, nil))
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	test.Cleanup(billing.Stop)
	keyService := service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg)
	openAI := handler.ProvideOpenAIGatewayHandler(gateway, service.NewUpstreamJobService(jobs, nil, service.GatewayResourceJobCharge), nil, service.NewConcurrencyService(&realtimeRouteConcurrency{}), billing, keyService, nil, nil, nil, nil, nil, cfg, nil)
	group := &service.Group{VideoModelPrices: map[string]map[string]float64{"wan2.6-t2v": {"720p": 0.1}, "wanx2.1-vace-plus": {"720p": 0.1}}, ID: 3, Platform: groupPlatform, Status: service.StatusActive, Hydrated: true, RateMultiplier: 1}
	key := &service.APIKey{ID: 2, UserID: 1, GroupID: &group.ID, Group: group, User: &service.User{ID: 1, Status: service.StatusActive}}
	var resolver *service.CompositeRouteResolver
	if groupPlatform == service.PlatformComposite {
		resolver = service.NewCompositeRouteResolver(compositeRouteRepoStub{routes: []service.CompositeModelRoute{{ID: 1, GroupID: 3, PublicModel: publicModel, MatchType: service.CompositeRouteMatchExact, TargetPlatform: account.Platform, UpstreamModel: targetModel, Endpoint: service.CompositeRouteEndpointAny, Enabled: true}}})
	}
	router := gin.New()
	RegisterGatewayRoutes(router, &handler.Handlers{Gateway: &handler.GatewayHandler{}, OpenAIGateway: openAI}, middleware.APIKeyAuthMiddleware(func(request *gin.Context) {
		request.Set(string(middleware.ContextKeyAPIKey), key)
		request.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1, Concurrency: 1})
		request.Next()
	}), keyService, nil, nil, nil, resolver, cfg)
	return router
}

func TestProductionRealtimeModelSelection(test *testing.T) {
	for _, platform := range []string{service.PlatformQianwen, service.PlatformOpenAI} {
		wireModel := "qwen3-tts-flash-realtime-manual"
		if platform == service.PlatformOpenAI {
			wireModel = "gpt-realtime-manual"
		}
		for _, scenario := range []struct{ name, group, requested, target, channel, selected string }{
			{"manual", platform, wireModel, "", "", wireModel},
			{"account alias", platform, "selected-alias", "", "", "selected-alias"},
			{"channel fallback", platform, "public-alias", "", "selected-alias", "selected-alias"},
			{"explicit target beats channel", service.PlatformComposite, "public-alias", "selected-alias", "wrong-channel", "selected-alias"},
			{"empty target uses channel", service.PlatformComposite, "public-alias", "", "selected-alias", "selected-alias"},
		} {
			test.Run(platform+"/"+scenario.name, func(test *testing.T) {
				account := service.Account{ID: 7, Platform: platform, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 2, Credentials: map[string]any{"api_key": "fixture", "model_mapping": map[string]any{scenario.selected: wireModel}}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: 3}}}
				jobs := &realtimeRouteJobs{}
				router := realtimeProductionRouter(test, account, scenario.group, scenario.requested, scenario.target, scenario.channel, jobs)
				request := httptest.NewRequest(http.MethodGet, "/v1/realtime?model="+scenario.requested, nil)
				request.Header.Set("Connection", "Upgrade")
				request.Header.Set("Upgrade", "websocket")
				request.Header.Set("Sec-WebSocket-Version", "13")
				request.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)
				require.Equal(test, 503, recorder.Code, recorder.Body.String())
				require.NotNil(test, jobs.session, "must reach real handler session admission: %s", recorder.Body.String())
				require.Equal(test, scenario.requested, jobs.session.Billing.RequestedModel)
				require.Equal(test, wireModel, jobs.session.Billing.Model)
				require.Empty(test, jobs.session.Billing.Realtime.UpstreamResponseModel)
			})
		}
	}
}

func TestProductionRealtimeRejectsRevokedAndUnsupportedSelection(test *testing.T) {
	for _, platform := range []string{service.PlatformQianwen, service.PlatformOpenAI} {
		for _, scenario := range []string{"unsupported wire", "disabled account", "revoked selection", "invalid credentials", "unselected model", "OAuth is not API realtime"} {
			if scenario == "unsupported wire" && platform == service.PlatformOpenAI || scenario == "OAuth is not API realtime" && platform != service.PlatformOpenAI {
				continue
			}
			test.Run(platform+"/"+scenario, func(test *testing.T) {
				wireModel := "qwen3-tts-flash-realtime"
				if platform == service.PlatformOpenAI {
					wireModel = "gpt-realtime"
				}
				account := service.Account{ID: 7, Platform: platform, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 2, Credentials: map[string]any{"api_key": "fixture", "model_mapping": map[string]any{"selected-alias": wireModel}}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: 3}}}
				expected := 503
				switch scenario {
				case "unsupported wire":
					account.Credentials["model_mapping"] = map[string]any{"selected-alias": "qwen-text-only"}
					expected = 422
				case "disabled account":
					account.Status = service.StatusDisabled
				case "revoked selection":
					account.Extra = map[string]any{service.UpstreamModelAllowlistExtraKey: service.AccountModelAllowlist{Enabled: true, Models: []string{"different-model"}}}
				case "invalid credentials":
					account.Status = service.StatusError
				case "unselected model":
					account.Credentials["model_mapping"] = map[string]any{"different-model": wireModel}
				case "OAuth is not API realtime":
					account.Type = service.AccountTypeOAuth
				}
				jobs := &realtimeRouteJobs{}
				router := realtimeProductionRouter(test, account, platform, "selected-alias", "", "", jobs)
				request := httptest.NewRequest(http.MethodGet, "/v1/realtime?model=selected-alias", nil)
				request.Header.Set("Connection", "Upgrade")
				request.Header.Set("Upgrade", "websocket")
				request.Header.Set("Sec-WebSocket-Version", "13")
				request.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)
				require.Equal(test, expected, recorder.Code, recorder.Body.String())
				require.Nil(test, jobs.session)
			})
		}
	}
}

func TestProductionQianwenActionBindings(test *testing.T) {
	for _, groupPlatform := range []string{service.PlatformQianwen, service.PlatformComposite} {
		account := service.Account{Platform: service.PlatformQianwen}
		router := realtimeProductionRouter(test, account, groupPlatform, "public-alias", "qwen3-tts-flash-realtime", "", &realtimeRouteJobs{})
		for _, scenario := range []struct {
			method, path, body, contains string
			status                       int
		}{
			{"POST", "/v1/images/edits", `{"model":"public-alias"}`, "multipart", 400},
			{"POST", "/v1/audio/transcriptions", `{"model":"public-alias"}`, "multipart", 400},
			{"POST", "/v1/audio/speech", `{"model":"public-alias"}`, "input.text", 400},
			{"GET", "/v1/realtime?model=public-alias", "", "WebSocket upgrade required", 426},
			{"POST", "/v1/realtime?model=public-alias", "", "", 404},
			{"POST", "/v1/videos/edits", `{"model":"public-alias"}`, "action_not_implemented", 400},
			{"POST", "/v1/videos/extensions", `{"model":"public-alias"}`, "action_not_implemented", 400},
			{"POST", "/v1/audio/translations", `{"model":"public-alias"}`, "action_not_implemented", 400},
		} {
			test.Run(groupPlatform+scenario.method+scenario.path, func(test *testing.T) {
				request := httptest.NewRequest(scenario.method, scenario.path, strings.NewReader(scenario.body))
				request.Header.Set("Content-Type", "application/json")
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)
				require.Equal(test, scenario.status, recorder.Code, recorder.Body.String())
				require.Contains(test, recorder.Body.String(), scenario.contains)
			})
		}
	}
}

func (repository *realtimeRouteJobs) InsertRealtimeReceipt(_ context.Context, job *service.UpstreamJob) (*service.UpstreamJob, error) {
	if repository.receiptError != nil {
		return nil, repository.receiptError
	}
	if repository.storedReceipt != nil && repository.storedReceipt.UpstreamTaskID == job.UpstreamTaskID {
		if !service.EqualQianwenRealtimeReceipt(repository.storedReceipt, job) {
			return repository.storedReceipt, service.ErrQianwenRealtimeConflict
		}
		return repository.storedReceipt, nil
	}
	repository.storedReceipt = job
	repository.receipt <- job
	return job, nil
}
func (repository *realtimeRouteJobs) WithLocked(_ context.Context, id string, update func(*service.UpstreamJob, service.UpstreamJobTx) error) (*service.UpstreamJob, error) {
	job := repository.session
	if repository.storedReceipt != nil && repository.storedReceipt.PublicRequestID == id {
		job = repository.storedReceipt
	}
	err := update(job, repository)
	if job == repository.session && job.Terminal() && repository.finished != nil {
		close(repository.finished)
	}
	return job, err
}
func (repository *realtimeRouteJobs) Get(context.Context, string, int64, int64) (*service.UpstreamJob, error) {
	return nil, service.ErrUpstreamJobNotFound
}

type realtimeRouteTPM struct {
	service.UsageSettlementReceiptRepository
}

func (*realtimeRouteTPM) ReserveTPMAttempt(context.Context, service.TPMReservationCommand) (*service.TPMReservation, error) {
	return nil, nil
}
func (*realtimeRouteTPM) SettleTPMAttempt(context.Context, service.TPMReservationIdentity, int64) error {
	return nil
}
func (*realtimeRouteTPM) ReleaseUnsentTPMAttempt(context.Context, service.TPMReservationIdentity) error {
	return nil
}
func (*realtimeRouteTPM) MarkTPMAttemptUnknown(context.Context, service.TPMReservationIdentity) error {
	return nil
}

func TestProductionOpenAIRealtimeWireAndReceiptFacts(test *testing.T) {
	for _, groupPlatform := range []string{service.PlatformOpenAI, service.PlatformComposite} {
		test.Run(groupPlatform, func(test *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			sentModels := make(chan string, 1)
			provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				sentModels <- request.URL.Query().Get("model")
				connection, err := ws.Accept(writer, request, nil)
				if err != nil {
					return
				}
				defer connection.CloseNow()
				_ = connection.Write(ctx, ws.MessageText, []byte(`{"type":"session.created","session":{"id":"fixture-session","model":"provider-declared-snapshot"}}`))
				_, _, err = connection.Read(ctx)
				if err != nil {
					return
				}
				_ = connection.Write(ctx, ws.MessageText, []byte(`{"type":"response.done","response":{"id":"fixture-turn","status":"completed","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3,"input_token_details":{"text_tokens":1,"audio_tokens":0,"cached_tokens":0},"output_token_details":{"text_tokens":2,"audio_tokens":0}}}}`))
				_, _, _ = connection.Read(ctx)
			}))
			defer provider.Close()
			requested := "selected-alias"
			target := ""
			channel := ""
			if groupPlatform == service.PlatformComposite {
				requested = "public-alias"
				target = "selected-alias"
				channel = "wrong-channel"
			}
			account := service.Account{ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 2, Credentials: map[string]any{"api_key": "fixture", "base_url": provider.URL, "model_mapping": map[string]any{"selected-alias": "gpt-realtime-manual"}}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: 3}}}
			jobs := &realtimeRouteJobs{receipt: make(chan *service.UpstreamJob, 1), finished: make(chan struct{})}
			server := httptest.NewServer(realtimeProductionRouter(test, account, groupPlatform, requested, target, channel, jobs))
			defer server.Close()
			client, response, err := ws.Dial(ctx, server.URL+"/v1/realtime?model="+requested, nil)
			if err != nil && response != nil {
				body, _ := io.ReadAll(response.Body)
				test.Fatalf("upgrade: %v: %s", err, body)
			}
			require.NoError(test, err)
			defer client.CloseNow()
			_, _, err = client.Read(ctx)
			require.NoError(test, err)
			require.NoError(test, client.Write(ctx, ws.MessageText, []byte(`{"type":"response.create"}`)))
			_, body, err := client.Read(ctx)
			require.NoError(test, err)
			require.Contains(test, string(body), "response.done")
			select {
			case sent := <-sentModels:
				require.Equal(test, "gpt-realtime-manual", sent)
			case <-ctx.Done():
				test.Fatal("upstream did not receive model")
			}
			select {
			case receipt := <-jobs.receipt:
				require.Equal(test, requested, receipt.Billing.RequestedModel)
				require.Equal(test, "gpt-realtime-manual", receipt.Billing.Model)
				require.Equal(test, "provider-declared-snapshot", receipt.Billing.Realtime.UpstreamResponseModel)
				require.Nil(test, receipt.ChargedUsageLogID)
				require.Equal(test, service.PricingStateUnpriced, receipt.Billing.PricingState)
			case <-ctx.Done():
				test.Fatal("missing receipt")
			}
			_ = client.CloseNow()
			select {
			case <-jobs.finished:
			case <-ctx.Done():
				test.Fatal("session did not release")
			}
		})
	}
}

func TestProductionWiredActionsRequireValidKey(test *testing.T) {
	router, _, _ := newKeyBillingRouteTestRouter(test, config.RunModeStandard)
	for _, route := range []string{"GET /v1/realtime?model=gpt-realtime", "POST /v1/images/edits", "POST /v1/videos", "GET /v1/videos/fixture", "GET /v1/videos/fixture/content", "POST /v1/videos/fixture/cancel", "POST /v1/audio/speech", "POST /v1/audio/transcriptions"} {
		method, path, _ := strings.Cut(route, " ")
		for _, key := range []string{"", "invalid-fixture-key"} {
			request := httptest.NewRequest(method, path, nil)
			if key != "" {
				request.Header.Set("Authorization", "Bearer "+key)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			require.Equal(test, 401, recorder.Code, route+recorder.Body.String())
		}
	}
}

// Only the TLS socket destination is replaced. The real handler constructs the
// pinned DashScope URL and relays its native event schema through both routes.
func TestProductionQianwenRealtimeNativeAndAliasWire(test *testing.T) {
	for _, path := range []string{"/v1/realtime", "/api-ws/v1/realtime"} {
		test.Run(path, func(test *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			wireModel := "qwen3-tts-flash-realtime-manual"
			sentModels := make(chan string, 1)
			updated := `{"type":"session.updated","session":{"mode":"commit"}}`
			provider := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				sentModels <- request.URL.Query().Get("model")
				connection, err := ws.Accept(writer, request, nil)
				if err != nil {
					return
				}
				defer connection.CloseNow()
				_ = connection.Write(ctx, ws.MessageText, []byte(`{"type":"session.created","session":{"id":"fixture-session","model":"`+wireModel+`"}}`))
				_, _, err = connection.Read(ctx)
				if err != nil {
					return
				}
				_ = connection.Write(ctx, ws.MessageText, []byte(updated))
				_, _, _ = connection.Read(ctx)
			}))
			defer provider.Close()
			originalTransport := http.DefaultTransport
			transport := originalTransport.(*http.Transport).Clone()
			transport.Proxy = nil
			transport.DialTLSContext = func(dialContext context.Context, network, address string) (net.Conn, error) {
				if address != "dashscope.aliyuncs.com:443" {
					return nil, errors.New("unexpected external socket destination")
				}
				dialer := tls.Dialer{Config: provider.Client().Transport.(*http.Transport).TLSClientConfig}
				return dialer.DialContext(dialContext, network, provider.Listener.Addr().String())
			}
			http.DefaultTransport = transport
			defer func() { http.DefaultTransport = originalTransport; transport.CloseIdleConnections() }()
			account := service.Account{ID: 7, Platform: service.PlatformQianwen, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 2, Credentials: map[string]any{"api_key": "fixture", "model_mapping": map[string]any{"selected-alias": wireModel}}, AccountGroups: []service.AccountGroup{{AccountID: 7, GroupID: 3}}}
			jobs := &realtimeRouteJobs{receipt: make(chan *service.UpstreamJob, 1), finished: make(chan struct{})}
			server := httptest.NewServer(realtimeProductionRouter(test, account, service.PlatformComposite, "public-alias", "selected-alias", "wrong-channel", jobs))
			defer server.Close()
			client, response, err := ws.Dial(ctx, server.URL+path+"?model=public-alias", nil)
			if err != nil && response != nil {
				body, _ := io.ReadAll(response.Body)
				test.Fatalf("upgrade: %v: %s", err, body)
			}
			require.NoError(test, err)
			defer client.CloseNow()
			_, created, err := client.Read(ctx)
			require.NoError(test, err)
			require.Contains(test, string(created), wireModel)
			require.NoError(test, client.Write(ctx, ws.MessageText, []byte(`{"type":"session.update","session":{"mode":"commit","voice":"Cherry","response_format":"pcm","sample_rate":24000}}`)))
			_, body, err := client.Read(ctx)
			require.NoError(test, err)
			require.Equal(test, updated, string(body), "DashScope event bytes must be preserved")
			require.Equal(test, wireModel, <-sentModels)
			require.Equal(test, "public-alias", jobs.session.Billing.RequestedModel)
			require.Equal(test, wireModel, jobs.session.Billing.Model)
			_ = client.CloseNow()
			select {
			case <-jobs.finished:
			case <-ctx.Done():
				test.Fatal("session did not release")
			}
		})
	}
}

func (repository *realtimeRouteJobs) Charge(_ context.Context, usage *service.UsageLog, command *service.UsageBillingCommand) error {
	repository.charges++
	usage.ID = int64(repository.charges)
	copied := *command
	repository.chargedCommand = &copied
	return nil
}
