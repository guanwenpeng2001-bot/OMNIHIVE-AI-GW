package service

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// openAIRawStreamTruncatedUpstreamMessage 是 raw CC 直转路径上游截断的 ops 消息。
const openAIRawStreamTruncatedUpstreamMessage = "Upstream Chat Completions stream ended before any terminal chunk"

// recordOpenAIRawStreamTruncation 把上游截断记入 ops 上下文，使其在错误日志与
// 账号健康度中可见——这正是此前"HTTP 200 假成功"丢掉的信息。
func recordOpenAIRawStreamTruncation(
	c *gin.Context,
	account *Account,
	upstreamRequestID string,
	cause error,
	kind string,
) {
	if c == nil {
		return
	}
	message := openAIRawStreamTruncatedMessage(cause)
	platform := PlatformOpenAI
	accountID := int64(0)
	accountName := ""
	if account != nil {
		platform = account.Platform
		accountID = account.ID
		accountName = account.Name
	}

	setOpsUpstreamError(c, http.StatusBadGateway, message, "")
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		ProxyID:            opsUpstreamProxyID(account),
		ProxyName:          opsUpstreamProxyName(account),
		Platform:           platform,
		AccountID:          accountID,
		AccountName:        accountName,
		UpstreamStatusCode: http.StatusBadGateway,
		UpstreamRequestID:  strings.TrimSpace(upstreamRequestID),
		Kind:               kind,
		Message:            message,
	})
}

// openAIRawStreamTruncatedMessage 拼出 ops 消息：干净 EOF 没有底层错误可带，
// 传输层错误（connection reset / http2 stream error）则保留原因以便定位。
func openAIRawStreamTruncatedMessage(cause error) string {
	if cause == nil || errors.Is(cause, ErrOpenAIUpstreamStreamTruncated) {
		return openAIRawStreamTruncatedUpstreamMessage
	}
	return openAIRawStreamTruncatedUpstreamMessage + ": " + cause.Error()
}
