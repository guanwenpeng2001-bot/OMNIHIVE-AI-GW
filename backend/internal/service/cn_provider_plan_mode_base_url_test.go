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
			"api_key": "sk-plan-mode-fixture", "account_mode": AccountModeCoding, "base_url": baseURL,
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
			"api_key": "sk-plan-mode-fixture", "account_mode": AccountModeCoding, "base_url": baseURL,
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
			"api_key": "sk-plan-mode-fixture", "account_mode": AccountModeCoding, "base_url": baseURL,
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

// 火山方舟与千帆的 messages 格子在能力矩阵里仍是 hard_conflict，账号选不到
// anthropic 协议（非多协议平台的 GetAPIProtocol 固定 chat_completions，
// GetAnthropicProtocolBaseURL 直接返回空串），所以这里钉住当前可达的那条路：
// Claude→chat 兼容桥落到 GetOpenAIBaseURL()，coding 模式下同样是套餐端点。
// 这不是永久不变量：一旦产品决定把这两格改成 implemented，本用例应改为断言
// 套餐的 Anthropic 兼容根（火山 https://ark.cn-beijing.volces.com/api/coding、
// 千帆 https://qianfan.baidubce.com/anthropic/tokenplan/personal，均 2026-09-23 抓取）。
func TestLatePlatformPlanModeAnthropicSurfaceStaysParkedForArkAndQianfan(t *testing.T) {
	t.Parallel()

	cases := []struct {
		platform      string
		codingChatURL string
	}{
		{PlatformVolcengine, DefaultVolcengineCodingBaseURL},
		{PlatformQianfan, DefaultQianfanCodingBaseURL},
	}
	for _, testCase := range cases {
		account := &Account{Platform: testCase.platform, Type: AccountTypeAPIKey, Credentials: map[string]any{
			"api_key": "sk-plan-mode-fixture", "account_mode": AccountModeCoding, "api_protocol": APIProtocolAnthropic,
		}}
		require.Equal(t, APIProtocolChatCompletions, account.GetAPIProtocol(), testCase.platform)
		require.Empty(t, account.GetAnthropicProtocolBaseURL(), testCase.platform)
		require.Equal(t, testCase.codingChatURL, account.GetOpenAIBaseURL(), testCase.platform)
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

// 套餐主机必须留在本厂商的 registry profile 上：落到 custom:<host> 会让 models.dev
// 元数据查找整体落空（火山 ark.*.volces.com 与千帆 qianfan.baidubce.com 原本就被
// 归类，混元的套餐主机 lkeap 需要显式归类）。
func TestLatePlatformPlanModeHostsKeepTheirPlatformProfile(t *testing.T) {
	t.Parallel()

	cases := []struct {
		platform string
		baseURL  string
		want     string
	}{
		{PlatformHunyuan, DefaultHunyuanCodingBaseURL, "hunyuan"},
		{PlatformHunyuan, DefaultHunyuanCodingAnthropicBaseURL, "hunyuan"},
		{PlatformHunyuan, "https://api.lkeap.cloud.tencent.com/plan/v3", "hunyuan"},
		{PlatformVolcengine, DefaultVolcengineCodingBaseURL, "volcengine"},
		{PlatformQianfan, DefaultQianfanCodingBaseURL, "qianfan"},
	}
	for _, testCase := range cases {
		account := &Account{Platform: testCase.platform, Type: AccountTypeAPIKey, Credentials: map[string]any{
			"api_key": "sk-plan-mode-fixture", "account_mode": AccountModeCoding, "base_url": testCase.baseURL,
		}}
		require.Equal(t, testCase.want, resolveCapabilityProfile(account), testCase.baseURL)
	}
}
