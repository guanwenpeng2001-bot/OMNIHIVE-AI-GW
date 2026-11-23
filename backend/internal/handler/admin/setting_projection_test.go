//go:build unit

package admin

import (
	"reflect"
	"strings"
	"testing"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/dto"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSystemSettingsProjectionCoversEveryDTOField(t *testing.T) {
	computed := map[string]bool{
		"TotpEncryptionKeyConfigured": true, "PasskeyConfigured": true, "PasskeyRPID": true,
		"PasskeyRPOrigins": true, "LoginAgreementDocuments": true, "CustomMenuItems": true,
		"CustomEndpoints": true, "DefaultSubscriptions": true, "AccountQuotaNotifyEmails": true,
	}
	paymentFields := map[string]bool{}
	for field := range map[string]string{
		"PaymentEnabled": "Enabled", "PaymentMinAmount": "MinAmount", "PaymentMaxAmount": "MaxAmount",
		"PaymentDailyLimit": "DailyLimit", "PaymentOrderTimeoutMin": "OrderTimeoutMin",
		"PaymentMaxPendingOrders": "MaxPendingOrders", "PaymentEnabledTypes": "EnabledTypes",
		"PaymentBalanceDisabled": "BalanceDisabled", "PaymentBalanceRechargeMultiplier": "BalanceRechargeMultiplier",
		"PaymentSubscriptionUSDToCNYRate": "SubscriptionUSDToCNYRate", "PaymentRechargeFeeRate": "RechargeFeeRate",
		"PaymentLoadBalanceStrat": "LoadBalanceStrategy", "PaymentProductNamePrefix": "ProductNamePrefix",
		"PaymentProductNameSuffix": "ProductNameSuffix", "PaymentHelpImageURL": "HelpImageURL",
		"PaymentHelpText": "HelpText", "PaymentCancelRateLimitEnabled": "CancelRateLimitEnabled",
		"PaymentCancelRateLimitMax": "CancelRateLimitMax", "PaymentCancelRateLimitWindow": "CancelRateLimitWindow",
		"PaymentCancelRateLimitUnit": "CancelRateLimitUnit", "PaymentCancelRateLimitMode": "CancelRateLimitMode",
		"PaymentAlipayForceQRCode": "AlipayForceQRCode", "PaymentAlipayMobilePrecreateDeepLink": "AlipayMobilePrecreateDeepLink",
	} {
		paymentFields[field] = true
	}
	typeOfDTO := reflect.TypeOf(dto.SystemSettings{})
	for index := 0; index < typeOfDTO.NumField(); index++ {
		name := typeOfDTO.Field(index).Name
		_, direct := systemSettingsProjection[name]
		require.True(t, direct || computed[name] || paymentFields[name] || name == "DefaultPlatformQuotas" || name == "OpenAIFastPolicySettings", "unprojected admin setting %s", name)
	}
}

func TestProjectSystemSettingsIncludesAuditedDivergentFields(t *testing.T) {
	payload := projectSystemSettings(&service.SystemSettings{OpenAITTFTMode: service.OpenAITTFTModeVisible, WebSearchEmulationEnabled: true})
	require.Equal(t, service.OpenAITTFTModeVisible, payload.OpenAITTFTMode)
	require.True(t, payload.WebSearchEmulationEnabled)
}

func TestUpdateSettingsRequestHasOwnerForWritableFields(t *testing.T) {
	requestType := reflect.TypeOf(UpdateSettingsRequest{})
	registry := map[string]bool{}
	for _, registration := range NewSettingService(nil, nil).SettingRegistry() {
		registry[registration.Key] = true
	}
	for index := 0; index < requestType.NumField(); index++ {
		name, _, _ := strings.Cut(requestType.Field(index).Tag.Get("json"), ",")
		if alias, ok := settingKeyJSONAliases[name]; ok {
			name = alias
		}
		switch name {
		case "smtp_password", "turnstile_secret_key", "tencent_captcha_app_secret_key", "tencent_captcha_cloud_secret_key", "aliyun_captcha_access_key_secret", "linuxdo_connect_client_secret", "dingtalk_connect_client_secret", "wechat_connect_app_secret", "wechat_connect_open_app_secret", "wechat_connect_mp_app_secret", "wechat_connect_mobile_app_secret", "oidc_connect_client_secret", "github_oauth_client_secret", "google_oauth_client_secret":
			require.True(t, registry[name], "secret has no registry owner: %s", name)
		default:
			require.True(t, registry[name] || strings.HasPrefix(name, "payment_") || strings.Contains(name, "platform_quotas") || name == "openai_fast_policy_settings", "write field has no registry owner: %s", name)
		}
	}
}
