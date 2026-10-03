//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// 后期平台（混元 TokenHub / 火山方舟 Ark / 千帆 ModelBuilder）的套餐接入模式：
// 厂商为 Coding Plan / Token Plan 发布了独立 Base URL 与专属 API Key，所以
// account_mode 决定默认端点；凭据里的显式 base_url 永远优先于模式默认值。
// 官方抓取日期 2026-09-23，逐条证据见 domain/hunyuan_actions.go、
// domain/volcengine_actions.go、domain/qianfan_actions.go 的证据串。
func TestLatePlatformPlanModeDefaultBaseURLs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		platform string
		mode     string
		want     string
	}{
		{"hunyuan unset mode", PlatformHunyuan, "", hunyuanCompatibilityBaseURL},
		{"hunyuan payg", PlatformHunyuan, AccountModePayG, hunyuanCompatibilityBaseURL},
		{"hunyuan coding", PlatformHunyuan, AccountModeCoding, DefaultHunyuanCodingBaseURL},
		{"volcengine unset mode", PlatformVolcengine, "", volcengineRuntimeBaseURL},
		{"volcengine payg", PlatformVolcengine, AccountModePayG, volcengineRuntimeBaseURL},
		{"volcengine coding", PlatformVolcengine, AccountModeCoding, DefaultVolcengineCodingBaseURL},
		{"qianfan unset mode", PlatformQianfan, "", qianfanDefaultBaseURL},
		{"qianfan payg", PlatformQianfan, AccountModePayG, qianfanDefaultBaseURL},
		{"qianfan coding", PlatformQianfan, AccountModeCoding, DefaultQianfanCodingBaseURL},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			credentials := map[string]any{"api_key": "sk-plan-mode-fixture"}
			if testCase.mode != "" {
				credentials["account_mode"] = testCase.mode
			}
			account := &Account{Platform: testCase.platform, Type: AccountTypeAPIKey, Credentials: credentials}
			require.Equal(t, testCase.want, account.GetOpenAIBaseURL())
		})
	}
}

// 显式 base_url 覆盖模式默认值（既有语义：用户填了就用他的）。
func TestLatePlatformPlanModeExplicitBaseURLWins(t *testing.T) {
	t.Parallel()

	for _, platform := range []string{PlatformHunyuan, PlatformVolcengine, PlatformQianfan} {
		account := &Account{Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{
			"api_key":      "sk-plan-mode-fixture",
			"account_mode": AccountModeCoding,
			"base_url":     "https://relay.example/v1",
		}}
		require.Equal(t, "https://relay.example/v1", account.GetOpenAIBaseURL(), platform)
	}
}

// 套餐默认值必须落到真实转发路径上：这三家的 chat 干线不走 GetOpenAIBaseURL，
// 而是各自的适配器（混元 buildHunyuanChatRequest、千帆 buildQianfanRequest、
// 火山 prepareVolcengineChatRequest）。base_url 为空 + coding 模式时，出站 URL
// 必须是套餐端点；写入套餐端点作为显式 base_url 时同样必须被接受。
func TestLatePlatformPlanModeReachesTheChatWire(t *testing.T) {
	t.Parallel()

	identityPolicy := func(base string) (string, error) { return base, nil }
	hunyuanBody := []byte(`{"model":"hunyuan-turbos-latest","messages":[{"role":"user","content":"hi"}],"stream":false}`)
	qianfanBody := []byte(`{"model":"ernie-4.5-turbo","messages":[{"role":"user","content":"hi"}]}`)
	volcengineBody := []byte(`{"model":"ep-plan-mode-fixture"}`)

	t.Run("hunyuan", func(t *testing.T) {
		account := &Account{Platform: PlatformHunyuan, Type: AccountTypeAPIKey, Credentials: map[string]any{
			"api_key":      "sk-plan-mode-fixture",
			"account_mode": AccountModeCoding,
		}}
		request, err := buildHunyuanChatRequest(context.Background(), account, hunyuanBody, "hunyuan-turbos-latest")
		require.NoError(t, err)
		require.Equal(t, DefaultHunyuanCodingBaseURL+"/chat/completions", request.URL.String())

		// 前端会把选中的站点写进 base_url；套餐站点必须在官方白名单里而不是被拒。
		account.Credentials["base_url"] = DefaultHunyuanCodingBaseURL
		request, err = buildHunyuanChatRequest(context.Background(), account, hunyuanBody, "hunyuan-turbos-latest")
		require.NoError(t, err)
		require.Equal(t, DefaultHunyuanCodingBaseURL+"/chat/completions", request.URL.String())
	})

	t.Run("volcengine", func(t *testing.T) {
		account := &Account{Platform: PlatformVolcengine, Type: AccountTypeAPIKey, Credentials: map[string]any{
			"api_key":      "sk-plan-mode-fixture",
			"account_mode": AccountModeCoding,
		}}
		prepared, err := (&OpenAIGatewayService{}).prepareVolcengineChatRequest(account, volcengineBody, "ep-plan-mode-fixture")
		require.NoError(t, err)
		require.Equal(t, DefaultVolcengineCodingBaseURL+"/chat/completions", prepared.TargetURL)

		account.Credentials["base_url"] = DefaultVolcengineCodingBaseURL
		prepared, err = (&OpenAIGatewayService{}).prepareVolcengineChatRequest(account, volcengineBody, "ep-plan-mode-fixture")
		require.NoError(t, err)
		require.Equal(t, DefaultVolcengineCodingBaseURL+"/chat/completions", prepared.TargetURL)
	})

	t.Run("qianfan", func(t *testing.T) {
		account := &Account{Platform: PlatformQianfan, Type: AccountTypeAPIKey, Credentials: map[string]any{
			"api_key":      "sk-plan-mode-fixture",
			"account_mode": AccountModeCoding,
		}}
		request, err := buildQianfanRequest(context.Background(), account, "chat/completions", qianfanBody, identityPolicy)
		require.NoError(t, err)
		require.Equal(t, DefaultQianfanCodingBaseURL+"/chat/completions", request.URL.String())

		// 企业版 Token Plan 没有第三级选择器，由用户显式填 base_url 覆盖。
		account.Credentials["base_url"] = "https://qianfan.baidubce.com/v2/tokenplan/team"
		request, err = buildQianfanRequest(context.Background(), account, "chat/completions", qianfanBody, identityPolicy)
		require.NoError(t, err)
		require.Equal(t, "https://qianfan.baidubce.com/v2/tokenplan/team/chat/completions", request.URL.String())
	})
}

// 套餐支持是"接受官方套餐端点"，不是放开路径校验：非官方主机、管理面路径和
// 端点式的 base_url 仍然必须在出站前被拒。
func TestLatePlatformPlanModeKeepsChatBaseURLGuards(t *testing.T) {
	t.Parallel()

	identityPolicy := func(base string) (string, error) { return base, nil }
	hunyuanBody := []byte(`{"model":"hunyuan-turbos-latest","messages":[{"role":"user","content":"hi"}]}`)
	qianfanBody := []byte(`{"model":"ernie-4.5-turbo","messages":[{"role":"user","content":"hi"}]}`)
	volcengineBody := []byte(`{"model":"ep-plan-mode-fixture"}`)

	for _, baseURL := range []string{
		"https://api.lkeap.cloud.tencent.com.evil.example/coding/v3",
		"https://relay.example/coding/v3",
	} {
		account := &Account{Platform: PlatformHunyuan, Type: AccountTypeAPIKey, Credentials: map[string]any{
			"api_key": "sk-plan-mode-fixture", "account_mode": AccountModeCoding, "api_protocol": APIProtocolChatCompletions, "base_url": baseURL,
		}}
		_, err := buildHunyuanChatRequest(context.Background(), account, hunyuanBody, "hunyuan-turbos-latest")
		require.Error(t, err, baseURL)
	}

	for _, baseURL := range []string{
		// 套餐专用的 Anthropic 兼容根不是 chat base。
		"https://ark.cn-beijing.volces.com/api/coding",
		"https://ark.cn-beijing.volces.com/api/coding/v3/models",
		"https://ark.cn-beijing.volces.com/api/v3/models",
	} {
		account := &Account{Platform: PlatformVolcengine, Type: AccountTypeAPIKey, Credentials: map[string]any{
			"api_key": "sk-plan-mode-fixture", "account_mode": AccountModeCoding, "api_protocol": APIProtocolChatCompletions, "base_url": baseURL,
		}}
		_, err := (&OpenAIGatewayService{}).prepareVolcengineChatRequest(account, volcengineBody, "ep-plan-mode-fixture")
		require.Error(t, err, baseURL)
	}

	for _, baseURL := range []string{
		"https://qianfan.baidubce.com/v2/tokenplan/personal/chat/completions",
		"https://qianfan.baidubce.com/v2/tokenplan",
		"https://relay.example/v2/chat/completions",
	} {
		account := &Account{Platform: PlatformQianfan, Type: AccountTypeAPIKey, Credentials: map[string]any{
			"api_key": "sk-plan-mode-fixture", "account_mode": AccountModeCoding, "api_protocol": APIProtocolChatCompletions, "base_url": baseURL,
		}}
		_, err := buildQianfanRequest(context.Background(), account, "chat/completions", qianfanBody, identityPolicy)
		require.Error(t, err, baseURL)
	}
}

// 混元的 messages 格子已绑定原生 Anthropic 协议：套餐账号必须走厂商的套餐专用
// Anthropic 兼容根，而不是按量付费的 TokenHub 站点。
func TestLatePlatformPlanModeAnthropicBaseURLs(t *testing.T) {
	t.Parallel()

	newHunyuan := func(mode, protocol string) *Account {
		return &Account{Platform: PlatformHunyuan, Type: AccountTypeAPIKey, Credentials: map[string]any{
			"api_key": "sk-plan-mode-fixture", "account_mode": mode, "api_protocol": protocol,
		}}
	}

	payg := newHunyuan(AccountModePayG, APIProtocolAnthropic)
	coding := newHunyuan(AccountModeCoding, APIProtocolAnthropic)
	require.Equal(t, hunyuanCompatibilityBaseURL, payg.GetAnthropicProtocolBaseURL())
	require.Equal(t, DefaultHunyuanCodingAnthropicBaseURL, coding.GetAnthropicProtocolBaseURL())
	require.NotEqual(t, payg.GetAnthropicProtocolBaseURL(), coding.GetAnthropicProtocolBaseURL())

	// 出站路径由 nativeAnthropicTargetURL 版本感知拼接：{base}/v1/messages。
	codingTargetURL, err := (&OpenAIGatewayService{}).nativeAnthropicTargetURL(coding)
	require.NoError(t, err)
	require.Equal(t, DefaultHunyuanCodingAnthropicBaseURL+"/v1/messages", codingTargetURL)
	paygTargetURL, err := (&OpenAIGatewayService{}).nativeAnthropicTargetURL(payg)
	require.NoError(t, err)
	require.Equal(t, hunyuanCompatibilityBaseURL+"/messages", paygTargetURL)

	// adaptive 档与显式 anthropic 档取同一个套餐根。
	adaptive := newHunyuan(AccountModeCoding, APIProtocolAdaptive)
	require.Equal(t, DefaultHunyuanCodingAnthropicBaseURL, adaptive.GetCNProtocolBaseURL(APIProtocolAnthropic))
	require.Equal(t, DefaultHunyuanCodingAnthropicBaseURL, adaptive.GetAnthropicProtocolBaseURL())

	// OpenAI 格式路径（模型同步等）仍回落套餐的 OpenAI 兼容根，而不是 Anthropic 根。
	require.Equal(t, DefaultHunyuanCodingBaseURL, coding.GetOpenAIFormatBaseURL())

	// 显式 base_url 覆盖模式默认值。
	explicit := newHunyuan(AccountModeCoding, APIProtocolAnthropic)
	explicit.Credentials["base_url"] = "https://relay.example/anthropic"
	require.Equal(t, "https://relay.example/anthropic", explicit.GetAnthropicProtocolBaseURL())
}

// Explicit plan Anthropic protocols use the documented native plan roots.
func TestLatePlatformPlanModeAnthropicSurface(t *testing.T) {
	for _, tc := range []struct{ platform, base string }{
		{PlatformVolcengine, DefaultVolcengineCodingAnthropicBaseURL},
		{PlatformQianfan, DefaultQianfanCodingAnthropicBaseURL},
	} {
		account := &Account{Platform: tc.platform, Type: AccountTypeAPIKey, Credentials: map[string]any{
			"api_key": "fixture", "account_mode": AccountModeCoding, "api_protocol": APIProtocolAnthropic,
		}}
		require.Equal(t, APIProtocolAnthropic, account.GetAPIProtocol())
		require.Equal(t, tc.base, account.GetAnthropicProtocolBaseURL())
	}
}

// 千帆 Token Plan 的两条专属子路径都是版本根，不再被追加 /v2；常规地址的
// 版本规则（必须落在 /v2、拒绝第二个版本段）保持不变。
func TestQianfanTokenPlanBaseURLIsAVersionRoot(t *testing.T) {
	t.Parallel()

	identityPolicy := func(base string) (string, error) { return base, nil }
	body := []byte(`{"model":"ernie-4.5-turbo","messages":[{"role":"user","content":"hi"}]}`)

	for _, testCase := range []struct {
		base string
		want string
	}{
		{DefaultQianfanCodingBaseURL, DefaultQianfanCodingBaseURL + "/chat/completions"},
		{"https://qianfan.baidubce.com/v2/tokenplan/team", "https://qianfan.baidubce.com/v2/tokenplan/team/chat/completions"},
		{"https://qianfan.baidubce.com/v2", "https://qianfan.baidubce.com/v2/chat/completions"},
		{"https://relay.example/prefix", "https://relay.example/prefix/v2/chat/completions"},
	} {
		account := &Account{Platform: PlatformQianfan, Type: AccountTypeAPIKey, Credentials: map[string]any{
			"api_key": "sk-plan-mode-fixture", "base_url": testCase.base,
		}}
		request, err := buildQianfanRequest(context.Background(), account, "chat/completions", body, identityPolicy)
		require.NoError(t, err, testCase.base)
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, testCase.want, request.URL.String())
	}
}

// Routing evidence for a plan does not establish a PAYG metadata identity.
// Unknown plan metadata must remain unknown until independently pinned.
func TestLatePlatformPlanModeHostsKeepUnknownMetadata(t *testing.T) {
	t.Parallel()

	cases := []struct {
		platform string
		baseURL  string
	}{
		{PlatformHunyuan, DefaultHunyuanCodingBaseURL},
		{PlatformHunyuan, DefaultHunyuanCodingAnthropicBaseURL},
		{PlatformHunyuan, "https://api.lkeap.cloud.tencent.com/plan/v3"},
		{PlatformVolcengine, DefaultVolcengineCodingBaseURL},
		{PlatformQianfan, DefaultQianfanCodingBaseURL},
	}
	for _, testCase := range cases {
		account := &Account{Platform: testCase.platform, Type: AccountTypeAPIKey, Credentials: map[string]any{
			"api_key": "sk-plan-mode-fixture", "account_mode": AccountModeCoding, "base_url": testCase.baseURL,
		}}
		metadataBase := testCase.baseURL
		if metadataBase == DefaultHunyuanCodingAnthropicBaseURL {
			metadataBase = DefaultHunyuanCodingBaseURL
		}
		require.Equal(t, "custom:"+testCase.platform+":"+metadataBase, resolveCapabilityProfile(account), testCase.baseURL)
	}
}
