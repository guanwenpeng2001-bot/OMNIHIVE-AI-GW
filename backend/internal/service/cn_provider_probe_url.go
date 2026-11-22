package service

// CN 供应商探测端点的出站 URL 安全策略校验（配额/余额探测共用）。
//
// 背景（review B4）：这两条探测路径会把账号 API key 发往 base_url 衍生端点，
// 此前完全绕过 security.url_allowlist——在加固部署里构成任意外发与内网探测
// 面（本项目此前发生过账号测试 SSRF 生产事件）。与网关转发
// （validateUpstreamBaseURL）、Grok 探测（grokOperatorPolicyValidator）一致，
// 探测发起前必须过同一套运营者策略。

import (
	"errors"
	"fmt"
	"strings"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/config"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/util/urlvalidator"
)

// cnValidateProbeURL 按全局出站 URL 安全策略校验探测端点，返回规范化 URL。
// 白名单开启时强制 UpstreamHosts；关闭时仅取消主机名限制，URL
// 校验仍保留 HTTPS 和私网策略；cfg 为 nil 时使用默认 HTTPS/公网策略。
func cnValidateProbeURL(cfg *config.Config, raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("probe url is required")
	}
	var allowInsecureHTTP bool
	var options urlvalidator.ValidationOptions
	if cfg != nil {
		allowInsecureHTTP = cfg.Security.URLAllowlist.AllowInsecureHTTP
		options = cfg.Security.URLAllowlist.UpstreamValidationOptions()
	}
	normalized, err := urlvalidator.ValidateHTTPURL(trimmed, allowInsecureHTTP, options)
	if err != nil {
		return "", fmt.Errorf("probe target rejected by URL security policy: %w", err)
	}
	return normalized, nil
}

// nativeProfileProbeURL authorizes only implemented, exact regional/plan probes.
// An empty result means unknown/unsupported, never invalid credentials.
func nativeProfileProbeURL(account *Account, quota bool) string {
	if !quota && account != nil && account.Platform == PlatformMiniMax {
		return miniMaxPayGBalanceURL(account)
	}
	p := nativeAccountProfile(account)
	if p == nil {
		return ""
	}
	if quota {
		if p.Mode != AccountModeCoding {
			return ""
		}
		switch p.Platform {
		case PlatformKimi:
			if p.Chat == DefaultKimiCodingBaseURL {
				return kimiQuotaURL(p.Chat)
			}
		case PlatformZhipu:
			return zhipuQuotaURL(p.Chat)
		case PlatformMiniMax:
			if strings.HasPrefix(p.Chat, "https://api.minimaxi.com/") || strings.HasPrefix(p.Chat, "https://api.minimax.io/") {
				return minimaxQuotaURL(p.Chat)
			}
		}
		return ""
	}
	if p.Mode != AccountModePayG {
		return ""
	}
	switch p.Platform {
	case PlatformKimi:
		return p.Chat + "/users/me/balance"
	case PlatformDeepseek:
		return "https://api.deepseek.com/user/balance"
	}
	return ""
}
