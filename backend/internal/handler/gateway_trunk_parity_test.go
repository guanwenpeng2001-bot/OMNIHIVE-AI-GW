package handler

// 两条主干（Claude GatewayHandler / OpenAI OpenAIGatewayHandler）差分测试：
// 同一输入在资格判定、重试耗尽与协议错误面上必须给出同一客户端可见结果。
// 差异只允许出现在下面显式标注的协议族格子里。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

// responsesEventIDPattern matches the per-response identifier that the Responses
// wire format generates for each response; it legitimately differs between two
// independent responses and must not count as a trunk divergence.
var responsesEventIDPattern = regexp.MustCompile(`resp_[0-9a-f]{32}`)

func normalizeResponsesEventIDs(body string) string {
	return responsesEventIDPattern.ReplaceAllString(body, "resp_NORMALIZED")
}

type trunkErrorResponse struct {
	status      int
	contentType string
	body        string
}

func captureTrunkErrorResponse(t *testing.T, requestPath string, handle func(c *gin.Context)) trunkErrorResponse {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, requestPath, nil)
	c.Request = c.Request.WithContext(context.Background())
	handle(c)
	return trunkErrorResponse{status: w.Code, contentType: w.Header().Get("Content-Type"), body: w.Body.String()}
}

// TestGatewayTrunksAgreeOnUpstreamClientErrorMapping 比较两条主干的上游状态码映射表。
// 唯一允许的差异是 529 的错误类型：Anthropic 主干报 overloaded_error，OpenAI 主干保持
// upstream_error；任何其它分支不一致都必须失败。
func TestGatewayTrunksAgreeOnUpstreamClientErrorMapping(t *testing.T) {
	claudeTrunk := &GatewayHandler{}
	openAITrunk := &OpenAIGatewayHandler{}

	statusCodes := []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusNotImplemented,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
		529,
		599,
	}
	for _, statusCode := range statusCodes {
		claudeStatus, claudeType, claudeMessage := claudeTrunk.mapUpstreamError(statusCode)
		openAIStatus, openAIType, openAIMessage := openAITrunk.mapUpstreamError(statusCode)

		require.Equal(t, claudeStatus, openAIStatus, "status %d must map to the same client status", statusCode)
		require.Equal(t, claudeMessage, openAIMessage, "status %d must map to the same client message", statusCode)
		if statusCode == 529 {
			require.Equal(t, "overloaded_error", claudeType, "529 is Anthropic-only overloaded_error")
			require.Equal(t, "upstream_error", openAIType, "529 stays upstream_error for the OpenAI trunk")
			continue
		}
		require.Equal(t, claudeType, openAIType, "status %d must map to the same error type", statusCode)
	}
}

// TestGatewayTrunksAgreeOnErrorWireShapes 比较两条主干在同一个入站协议下，非流式 JSON
// 与流内 SSE 两种形态写出的错误响应，包含 Responses 严格 SDK 要求的终止事件路径。
// 两条主干必须写出同一份响应；Responses 事件里每响应生成的 id 除外（归一化后比较）。
func TestGatewayTrunksAgreeOnErrorWireShapes(t *testing.T) {
	paths := []string{"/v1/messages", "/v1/responses"}
	for _, requestPath := range paths {
		for _, streamStarted := range []bool{false, true} {
			claudeResponse := captureTrunkErrorResponse(t, requestPath, func(c *gin.Context) {
				(&GatewayHandler{}).handleStreamingAwareError(c, http.StatusServiceUnavailable, "upstream_error", "upstream unavailable", streamStarted)
			})
			openAIResponse := captureTrunkErrorResponse(t, requestPath, func(c *gin.Context) {
				(&OpenAIGatewayHandler{}).handleStreamingAwareError(c, http.StatusServiceUnavailable, "upstream_error", "upstream unavailable", streamStarted)
			})

			require.Equal(t, claudeResponse.status, openAIResponse.status, "path %s streamStarted=%v", requestPath, streamStarted)
			require.Equal(t, claudeResponse.contentType, openAIResponse.contentType, "path %s streamStarted=%v", requestPath, streamStarted)
			require.Equal(t,
				normalizeResponsesEventIDs(claudeResponse.body),
				normalizeResponsesEventIDs(openAIResponse.body),
				"path %s streamStarted=%v must write the same error wire shape", requestPath, streamStarted)
		}
	}
}

// TestGatewayTrunksAgreeOnFailoverExhaustedResponse 比较重试耗尽后的兜底响应。两条主干在
// 同一个入站协议下必须写出同一份响应，唯一允许的差异是 529 错误码：Anthropic 主干报
// overloaded_error（见 mapUpstreamError），OpenAI 主干保持 upstream_error。
func TestGatewayTrunksAgreeOnFailoverExhaustedResponse(t *testing.T) {
	for _, requestPath := range []string{"/v1/messages", "/v1/responses"} {
		for _, statusCode := range []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusBadGateway, 529} {
			claudeResponse := captureTrunkErrorResponse(t, requestPath, func(c *gin.Context) {
				(&GatewayHandler{}).handleFailoverExhaustedSimple(c, statusCode, false)
			})
			openAIResponse := captureTrunkErrorResponse(t, requestPath, func(c *gin.Context) {
				(&OpenAIGatewayHandler{}).handleFailoverExhaustedSimple(c, statusCode, false)
			})

			require.Equal(t, claudeResponse.status, openAIResponse.status, "path %s upstream status %d", requestPath, statusCode)
			if statusCode != 529 {
				require.Equal(t, claudeResponse.body, openAIResponse.body, "path %s upstream status %d", requestPath, statusCode)
				continue
			}
			require.Contains(t, claudeResponse.body, "overloaded_error", "the Anthropic trunk reports 529 as overloaded_error")
			require.Contains(t, openAIResponse.body, "upstream_error", "the OpenAI trunk keeps 529 as upstream_error")
		}
	}
}

// TestGatewayTrunksShareUsageTaskSubmission 比较两条主干的计费任务提交语义：
// 未注入池、池已停止、任务 panic 三种情况下都必须执行一次且不 panic 逃逸。
func TestGatewayTrunksShareUsageTaskSubmission(t *testing.T) {
	runs := atomic.Int64{}
	newTask := func() service.UsageRecordTask {
		return func(context.Context) { runs.Add(1) }
	}
	panickingTask := func() service.UsageRecordTask {
		return func(context.Context) { panic("usage task failed") }
	}

	claudeTrunk := &GatewayHandler{}
	openAITrunk := &OpenAIGatewayHandler{}
	trunks := []struct {
		name       string
		submit     func(task service.UsageRecordTask)
		submitOnce func(task service.UsageRecordTask)
	}{
		{
			name:   "claude",
			submit: func(task service.UsageRecordTask) { claudeTrunk.submitUsageRecordTask(context.Background(), task) },
			submitOnce: func(task service.UsageRecordTask) {
				claudeTrunk.submitMandatoryUsageRecordTask(context.Background(), task)
			},
		},
		{
			name:   "openai",
			submit: func(task service.UsageRecordTask) { openAITrunk.submitUsageRecordTask(context.Background(), task) },
			submitOnce: func(task service.UsageRecordTask) {
				openAITrunk.submitMandatoryUsageRecordTask(context.Background(), task)
			},
		},
	}

	for _, trunk := range trunks {
		t.Run(trunk.name+"/no_pool_runs_inline", func(t *testing.T) {
			runs.Store(0)
			trunk.submit(newTask())
			require.EqualValues(t, 1, runs.Load(), "a billing task must still run exactly once without a worker pool")
		})

		t.Run(trunk.name+"/stopped_pool_falls_back_inline", func(t *testing.T) {
			runs.Store(0)
			stoppedPool := &service.UsageRecordWorkerPool{}
			task := newTask()
			if trunk.name == "claude" {
				(&GatewayHandler{usageRecordWorkerPool: stoppedPool}).submitUsageRecordTask(context.Background(), task)
			} else {
				(&OpenAIGatewayHandler{usageRecordWorkerPool: stoppedPool}).submitUsageRecordTask(context.Background(), task)
			}
			require.EqualValues(t, 1, runs.Load(), "a stopped pool must fall back to inline billing, not drop the task")
		})

		t.Run(trunk.name+"/panic_is_recovered", func(t *testing.T) {
			require.NotPanics(t, func() {
				trunk.submit(panickingTask())
			}, "billing task panics must be recovered by the shared submitter")
		})

		t.Run(trunk.name+"/mandatory_runs_once_per_request", func(t *testing.T) {
			runs.Store(0)
			trunk.submitOnce(newTask())
			trunk.submitOnce(newTask())
			require.EqualValues(t, 2, runs.Load(), "without a request plan every mandatory call persists its own task")
		})
	}
}

// TestGatewayTrunksKeepBillingTasksWithinBoundedTime 确认共享内联路径不会继承客户端
// 已取消的上下文：任务必须拿到一个可用的、有界的执行上下文。
func TestGatewayTrunksKeepBillingTasksWithinBoundedTime(t *testing.T) {
	contextChecks := make(chan bool, 2)
	parent, cancel := context.WithCancel(context.Background())
	cancel()

	task := func(ctx context.Context) {
		_, hasDeadline := ctx.Deadline()
		contextChecks <- hasDeadline && ctx.Err() == nil
	}

	(&GatewayHandler{}).submitUsageRecordTask(parent, task)
	(&OpenAIGatewayHandler{}).submitUsageRecordTask(parent, task)

	for index := 0; index < 2; index++ {
		select {
		case contextUsable := <-contextChecks:
			require.True(t, contextUsable, "inline billing must run on a fresh bounded context")
		case <-time.After(2 * time.Second):
			t.Fatal("inline billing task did not run")
		}
	}
}
