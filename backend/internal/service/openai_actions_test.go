package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ws "github.com/coder/websocket"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Synthetic protocol fixtures derived from official API reference examples,
// checked 2026-09-21; these are not recordings from a credentialed upstream.
func openAIActionAccount() *Account {
	return &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-fixture", "base_url": "https://api.openai.com/v1"}}
}

type openAIVideoAccountRepository struct {
	AccountRepository
	account *Account
}

func (r openAIVideoAccountRepository) GetByID(_ context.Context, id int64) (*Account, error) {
	if r.account == nil || r.account.ID != id {
		return nil, errors.New("account not found")
	}
	return r.account, nil
}

func openAIActionMultipart(t *testing.T, field, model string) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", model))
	require.NoError(t, writer.WriteField("timestamp_granularities[]", "word"))
	require.NoError(t, writer.WriteField("timestamp_granularities[]", "segment"))
	part, err := writer.CreateFormFile(field, "sample.wav")
	require.NoError(t, err)
	_, err = part.Write([]byte{0, 1, 255, 13, 10})
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return body.Bytes(), writer.FormDataContentType()
}

func TestOpenAIActionWireShapes(t *testing.T) {
	for _, fixture := range []struct{ action, endpoint, body, model string }{
		{"audio.tts", "/v1/audio/speech", `{"model":"public-tts","input":"Hello","voice":"alloy","response_format":"pcm"}`, "gpt-4o-mini-tts"},
		{"realtime.session", "/v1/realtime/client_secrets", `{"session":{"type":"realtime","model":"public-realtime"},"expires_after":{"anchor":"created_at","seconds":60}}`, "gpt-realtime"},
		{"video.generate", "/v1/videos", `{"model":"public-video","prompt":"Ocean","seconds":"4"}`, "sora-2"},
		{"video.edit", "/v1/videos/edits", `{"video":{"id":"video_fixture"},"prompt":"Make it blue"}`, "sora-2"},
		{"video.extend", "/v1/videos/extensions", `{"video":{"id":"video_fixture"},"prompt":"Continue","seconds":"4"}`, "sora-2"},
	} {
		t.Run(fixture.action, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"audio/pcm"}}, Body: io.NopCloser(bytes.NewReader([]byte{0, 255, 1}))}}
			service := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			response, err := service.ForwardOpenAIAction(context.Background(), openAIActionAccount(), fixture.action, "application/json", []byte(fixture.body), fixture.model)
			require.NoError(t, err)
			require.Equal(t, fixture.endpoint, upstream.lastReq.URL.Path)
			require.Equal(t, "api.openai.com", upstream.lastReq.URL.Host)
			require.Equal(t, "Bearer sk-fixture", upstream.lastReq.Header.Get("Authorization"))
			require.Equal(t, http.MethodPost, upstream.lastReq.Method)
			modelPath := "model"
			if fixture.action == "realtime.session" {
				modelPath = "session.model"
			}
			if fixture.action == "video.edit" || fixture.action == "video.extend" {
				require.JSONEq(t, fixture.body, string(upstream.lastBody))
			} else {
				require.Equal(t, fixture.model, gjson.GetBytes(upstream.lastBody, modelPath).String())
			}
			actual, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, []byte{0, 255, 1}, actual)
			require.NoError(t, response.Body.Close())
			require.Len(t, upstream.requests, 1)
		})
	}
}

func TestOpenAIActionMultipartPreservesBinaryAndRepeatedFields(t *testing.T) {
	for _, action := range []string{"audio.stt", "audio.translate", "image.variation"} {
		t.Run(action, func(t *testing.T) {
			field := "file"
			if action == "image.variation" {
				field = "image"
			}
			body, contentType := openAIActionMultipart(t, field, "public-model")
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"text":"Hello"}`))}}
			service := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			response, err := service.ForwardOpenAIAction(context.Background(), openAIActionAccount(), action, contentType, body, "whisper-1")
			require.NoError(t, err)
			defer func() { _ = response.Body.Close() }()
			endpoint, _ := openAIActionPath(action)
			require.Equal(t, "/v1"+endpoint, upstream.lastReq.URL.Path)
			_, parameters, err := mime.ParseMediaType(upstream.lastReq.Header.Get("Content-Type"))
			require.NoError(t, err)
			form, err := multipart.NewReader(bytes.NewReader(upstream.lastBody), parameters["boundary"]).ReadForm(1 << 20)
			require.NoError(t, err)
			defer func() { _ = form.RemoveAll() }()
			require.Equal(t, []string{"whisper-1"}, form.Value["model"])
			require.Equal(t, []string{"word", "segment"}, form.Value["timestamp_granularities[]"])
			require.Equal(t, "sample.wav", form.File[field][0].Filename)
			file, err := form.File[field][0].Open()
			require.NoError(t, err)
			defer func() { _ = file.Close() }()
			actual, err := io.ReadAll(file)
			require.NoError(t, err)
			require.Equal(t, []byte{0, 1, 255, 13, 10}, actual)
		})
	}
}

func TestOpenAIActionRejectsCredentialsAndMalformedBody(t *testing.T) {
	service := &OpenAIGatewayService{cfg: &config.Config{}}
	for _, kind := range []string{AccountTypeOAuth, AccountTypeSetupToken, AccountTypeServiceAccount} {
		account := openAIActionAccount()
		account.Type = kind
		_, err := service.ForwardOpenAIAction(context.Background(), account, "audio.tts", "application/json", []byte(`{"model":"tts-1"}`), "")
		require.ErrorIs(t, err, ErrOpenAIActionCredential)
	}
	for _, body := range []string{`{`, `[]`, `null`, `{}`} {
		_, err := service.ForwardOpenAIAction(context.Background(), openAIActionAccount(), "audio.tts", "application/json", []byte(body), "")
		require.Error(t, err)
	}
	body, contentType := openAIActionMultipart(t, "image", "whisper-1")
	_, err := service.ForwardOpenAIAction(context.Background(), openAIActionAccount(), "audio.stt", contentType, body, "")
	require.Error(t, err)
	_, err = service.ForwardOpenAIAction(context.Background(), openAIActionAccount(), "rerank", "application/json", []byte(`{"model":"x"}`), "")
	require.Error(t, err)
}

func TestOpenAIActionErrorsAreNotRetriedAndCancellationPropagates(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"rate_limit_exceeded"}}`))}}
	service := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	ctx, cancel := context.WithCancel(context.Background())
	response, err := service.ForwardOpenAIAction(ctx, openAIActionAccount(), "audio.tts", "application/json", []byte(`{"model":"tts-1","input":"hello","voice":"alloy"}`), "")
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	require.Equal(t, 429, response.StatusCode)
	require.Len(t, upstream.requests, 1)
	cancel()
	require.ErrorIs(t, upstream.lastReq.Context().Err(), context.Canceled)
}

type openAIActionResourceRepository struct {
	UpstreamJobRepository
	job *UpstreamJob
}

func (r openAIActionResourceRepository) Get(_ context.Context, _ string, userID, keyID int64) (*UpstreamJob, error) {
	if r.job.UserID != userID || r.job.APIKeyID != keyID {
		return nil, ErrUpstreamJobNotFound
	}
	return r.job, nil
}

func TestOpenAIActionResourceOwnershipAndNativeOperations(t *testing.T) {
	for _, fixture := range []struct{ family, operation, method, path string }{
		{"file", "content", "GET", "/v1/files/resource_fixture/content"}, {"file", "delete", "DELETE", "/v1/files/resource_fixture"},
		{"batch", "cancel", "POST", "/v1/batches/resource_fixture/cancel"}, {"batch", "status", "GET", "/v1/batches/resource_fixture"},
		{"video", "content", "GET", "/v1/videos/resource_fixture/content"}, {"video", "status", "GET", "/v1/videos/resource_fixture"},
	} {
		t.Run(fixture.family+fixture.operation, func(t *testing.T) {
			job := &UpstreamJob{Platform: PlatformOpenAI, Family: fixture.family, UserID: 1, APIKeyID: 2, AccountID: 42, UpstreamTaskID: "resource_fixture"}
			repository := openAIActionResourceRepository{job: job}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))}}
			service := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			_, err := service.ForwardOpenAIResource(context.Background(), repository, "public-fixture", 1, 3, openAIActionAccount(), fixture.operation)
			require.ErrorIs(t, err, ErrUpstreamJobNotFound)
			require.Empty(t, upstream.requests)
			response, err := service.ForwardOpenAIResource(context.Background(), repository, "public-fixture", 1, 2, openAIActionAccount(), fixture.operation)
			require.NoError(t, err)
			defer func() { _ = response.Body.Close() }()
			require.Equal(t, fixture.method, upstream.lastReq.Method)
			require.Equal(t, fixture.path, upstream.lastReq.URL.Path)
			job.UpstreamTaskID = "../other"
			_, err = service.ForwardOpenAIResource(context.Background(), repository, "public-fixture", 1, 2, openAIActionAccount(), fixture.operation)
			require.ErrorIs(t, err, ErrUpstreamJobNotFound)
			require.Len(t, upstream.requests, 1)
		})
	}
}

func TestOpenAIActionUsageAndLifecycleDoNotInventMeasurements(t *testing.T) {
	usage, observed := ParseOpenAIActionUsage([]byte(`{"response":{"usage":{"input_tokens":4,"output_tokens":2,"input_token_details":{"audio_tokens":3}}}}`))
	require.True(t, observed)
	require.Equal(t, int64(3), gjson.GetBytes(usage, "input_token_details.audio_tokens").Int())
	_, observed = ParseOpenAIActionUsage([]byte(`{"text":"hello"}`))
	require.False(t, observed)
	for _, status := range []string{"queued", "in_progress", "completed", "failed", "expired", "cancelled", "cancelling"} {
		update, err := ParseOpenAIJobUpdate([]byte(`{"id":"video_fixture","status":"` + status + `"}`))
		require.NoError(t, err)
		require.Equal(t, status, update.ProviderStatus)
	}
	_, err := ParseOpenAIJobUpdate([]byte(`{"id":"video_fixture","status":"unknown"}`))
	require.Error(t, err)
}

func TestOpenAIActionVoiceMultipartWireShape(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("name", "narrator"))
	require.NoError(t, writer.WriteField("consent", "cons_1234"))
	part, err := writer.CreateFormFile("audio_sample", "sample.wav")
	require.NoError(t, err)
	_, err = part.Write([]byte{1, 2, 3, 250})
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"voice_fixture","name":"narrator"}`))}}
	service := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	response, err := service.ForwardOpenAIAction(context.Background(), openAIActionAccount(), "voice", writer.FormDataContentType(), body.Bytes(), "")
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	require.Equal(t, "/v1/audio/voices", upstream.lastReq.URL.Path)
	require.Equal(t, "Bearer sk-fixture", upstream.lastReq.Header.Get("Authorization"))
	_, parameters, err := mime.ParseMediaType(upstream.lastReq.Header.Get("Content-Type"))
	require.NoError(t, err)
	form, err := multipart.NewReader(bytes.NewReader(upstream.lastBody), parameters["boundary"]).ReadForm(1 << 20)
	require.NoError(t, err)
	defer func() { _ = form.RemoveAll() }()
	require.Equal(t, []string{"narrator"}, form.Value["name"])
	require.Equal(t, []string{"cons_1234"}, form.Value["consent"])
	require.Empty(t, form.Value["model"], "voice creation has no model parameter")
	require.Equal(t, "sample.wav", form.File["audio_sample"][0].Filename)

	_, err = service.ForwardOpenAIAction(context.Background(), openAIActionAccount(), "voice", "application/json", []byte(`{"name":"narrator"}`), "")
	require.Error(t, err, "voice creation requires multipart")
	_, err = service.ForwardOpenAIAction(context.Background(), openAIActionAccount(), "voice", writer.FormDataContentType(), []byte("--"+parameters["boundary"]+"--\r\n"), "")
	require.Error(t, err, "voice creation requires the audio sample")
	oauthAccount := openAIActionAccount()
	oauthAccount.Type = AccountTypeOAuth
	_, err = service.ForwardOpenAIAction(context.Background(), oauthAccount, "voice", writer.FormDataContentType(), body.Bytes(), "")
	require.ErrorIs(t, err, ErrOpenAIActionCredential)
	require.Len(t, upstream.requests, 1, "local rejections never reach the upstream")
}

func TestOpenAIFileBatchSubmitAndPollWireShapes(t *testing.T) {
	var fileBody bytes.Buffer
	fileWriter := multipart.NewWriter(&fileBody)
	require.NoError(t, fileWriter.WriteField("purpose", "batch"))
	filePart, err := fileWriter.CreateFormFile("file", "input.jsonl")
	require.NoError(t, err)
	_, err = filePart.Write([]byte("{\"custom_id\":\"a\"}\n"))
	require.NoError(t, err)
	require.NoError(t, fileWriter.Close())

	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"file_abc123","object":"file","purpose":"batch","bytes":18,"filename":"input.jsonl"}`))}}
	service := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	update, err := service.SubmitOpenAIFile(context.Background(), openAIActionAccount(), fileWriter.FormDataContentType(), fileBody.Bytes())
	require.NoError(t, err)
	require.Equal(t, UpstreamJobUpdate{TaskID: "file_abc123", Status: "succeeded", ResultRef: "file_abc123"}, update)
	require.Equal(t, "/v1/files", upstream.lastReq.URL.Path)
	require.Equal(t, http.MethodPost, upstream.lastReq.Method)
	require.Contains(t, upstream.lastReq.Header.Get("Content-Type"), "multipart/form-data")

	_, err = service.SubmitOpenAIFile(context.Background(), openAIActionAccount(), "application/json", []byte(`{"purpose":"batch"}`))
	require.Error(t, err)
	upstream.resp = &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"file_abc123","object":"video"}`))}
	_, err = service.SubmitOpenAIFile(context.Background(), openAIActionAccount(), fileWriter.FormDataContentType(), fileBody.Bytes())
	require.Error(t, err, "a non-file object is not upload evidence")
	require.Len(t, upstream.requests, 2)

	batchBody := []byte(`{"input_file_id":"file_abc123","endpoint":"/v1/responses","completion_window":"24h"}`)
	upstream.resp = &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"batch_xyz","object":"batch","status":"validating","endpoint":"/v1/responses","input_file_id":"file_abc123"}`))}
	update, err = service.SubmitOpenAIBatch(context.Background(), openAIActionAccount(), batchBody)
	require.NoError(t, err)
	require.Equal(t, "batch_xyz", update.TaskID)
	require.Equal(t, "submitted", update.Status)
	require.Equal(t, "validating", update.ProviderStatus)
	require.Equal(t, "/v1/batches", upstream.lastReq.URL.Path)
	require.JSONEq(t, string(batchBody), string(upstream.lastBody))

	// Even an immediately completed batch stays running: the shared layer must
	// validate the output file before settlement.
	upstream.resp = &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"batch_xyz","object":"batch","status":"completed","output_file_id":"file_out1"}`))}
	update, err = service.SubmitOpenAIBatch(context.Background(), openAIActionAccount(), batchBody)
	require.NoError(t, err)
	require.Equal(t, "running", update.Status)

	for _, bad := range []string{
		`{"input_file_id":"file_abc123","endpoint":"/v1/search","completion_window":"24h"}`,
		`{"input_file_id":"file_abc123","endpoint":"/v1/responses","completion_window":"1h"}`,
		`{"input_file_id":"../secret","endpoint":"/v1/responses","completion_window":"24h"}`,
	} {
		_, err = service.SubmitOpenAIBatch(context.Background(), openAIActionAccount(), []byte(bad))
		require.Error(t, err, bad)
	}
	upstream.resp = &http.Response{StatusCode: 400, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"invalid file"}}`))}
	update, err = service.SubmitOpenAIBatch(context.Background(), openAIActionAccount(), batchBody)
	require.NoError(t, err)
	require.Equal(t, "failed", update.Status)
	require.Equal(t, "upstream_rejected", update.ErrorCode)

	job := &UpstreamJob{Platform: PlatformOpenAI, Family: "batch", AccountID: 42, UpstreamTaskID: "batch_xyz"}
	boundAccount := openAIActionAccount()
	boundAccount.Status = StatusActive
	service.accountRepo = openAIVideoAccountRepository{account: boundAccount}
	upstream.resp = &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"batch_xyz","object":"batch","status":"completed","output_file_id":"file_out1"}`))}
	update, err = service.PollOpenAIBatch(context.Background(), job)
	require.NoError(t, err)
	require.Equal(t, "succeeded", update.Status)
	require.Equal(t, "file_out1", update.ResultRef)
	require.Equal(t, "/v1/batches/batch_xyz", upstream.lastReq.URL.Path)
	require.Equal(t, http.MethodGet, upstream.lastReq.Method)

	upstream.resp = &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"batch_xyz","object":"batch","status":"cancelled"}`))}
	update, err = service.PollOpenAIBatch(context.Background(), job)
	require.NoError(t, err)
	require.Equal(t, "failed", update.Status)
	require.Equal(t, "upstream_cancelled", update.ErrorCode, "a cancelled batch may hold partial results; it is never a success")

	job.Platform = PlatformGrok
	_, err = service.PollOpenAIBatch(context.Background(), job)
	require.ErrorIs(t, err, ErrUpstreamJobNotFound)
	job.Platform = PlatformOpenAI
	job.Family = "video"
	_, err = service.PollOpenAIBatch(context.Background(), job)
	require.ErrorIs(t, err, ErrUpstreamJobUnsupportedUsage)
}

func TestOpenAIActionRealtimeHandshakeAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/realtime" || request.URL.Query().Get("model") != "gpt-realtime" || request.Header.Get("Authorization") != "Bearer sk-fixture" {
			writer.WriteHeader(401)
			return
		}
		connection, err := ws.Accept(writer, request, nil)
		if err != nil {
			return
		}
		defer func() { _ = connection.CloseNow() }()
		_ = connection.Write(request.Context(), ws.MessageText, []byte(`{"type":"session.created"}`))
		_, _, _ = connection.Read(request.Context())
	}))
	defer server.Close()
	account := openAIActionAccount()
	account.Credentials["base_url"] = server.URL
	// The loopback fixture uses plain http; production keeps the https default.
	service := &OpenAIGatewayService{cfg: loopbackHTTPTestConfig()}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	connection, err := service.OpenOpenAIRealtime(ctx, account, "gpt-realtime")
	require.NoError(t, err)
	defer func() { _ = connection.CloseNow() }()
	_, body, err := connection.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, "session.created", gjson.GetBytes(body, "type").String())
	cancel()
	_, _, err = connection.Read(ctx)
	require.Error(t, err)
	account.Type = AccountTypeOAuth
	_, err = service.OpenOpenAIRealtime(context.Background(), account, "gpt-realtime")
	require.ErrorIs(t, err, ErrOpenAIActionCredential)
}
