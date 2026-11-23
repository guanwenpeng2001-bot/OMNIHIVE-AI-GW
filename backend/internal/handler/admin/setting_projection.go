package admin

import (
	"context"
	"fmt"
	"reflect"

	"github.com/gin-gonic/gin"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/handler/dto"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/pkg/response"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
)

var systemSettingsProjection = buildSystemSettingsProjection()

func buildSystemSettingsProjection() map[string]string {
	serviceType := reflect.TypeOf(service.SystemSettings{})
	dtoType := reflect.TypeOf(dto.SystemSettings{})
	projection := make(map[string]string, dtoType.NumField())
	for index := 0; index < dtoType.NumField(); index++ {
		field := dtoType.Field(index)
		if _, exists := serviceType.FieldByName(field.Name); exists {
			if serviceTypeField, _ := serviceType.FieldByName(field.Name); serviceTypeField.Type != field.Type {
				continue
			}
			projection[field.Name] = field.Name
		}
	}
	return projection
}

func projectSystemSettings(settings *service.SystemSettings) dto.SystemSettings {
	payload := dto.SystemSettings{}
	source := reflect.ValueOf(settings)
	target := reflect.ValueOf(&payload).Elem()
	if source.IsValid() && !source.IsNil() {
		source = source.Elem()
		for dtoField, serviceField := range systemSettingsProjection {
			target.FieldByName(dtoField).Set(source.FieldByName(serviceField))
		}
	}
	return payload
}

// SystemSettingsProjection is the only successful GET/PUT response builder.
func (h *SettingHandler) SystemSettingsProjection(ctx context.Context) (map[string]any, error) {
	settings, err := h.settingService.GetAllSettings(ctx)
	if err != nil {
		return nil, err
	}
	authSourceDefaults, err := h.settingService.GetAuthSourceDefaultSettings(ctx)
	if err != nil {
		return nil, err
	}
	payload := projectSystemSettings(settings)

	passkeyConfigured, passkeyRPID, passkeyRPOrigins := h.settingService.PasskeyConfiguration()
	payload.TotpEncryptionKeyConfigured = h.settingService.IsTotpEncryptionKeyConfigured()
	payload.PasskeyConfigured = passkeyConfigured
	payload.PasskeyRPID = passkeyRPID
	payload.PasskeyRPOrigins = passkeyRPOrigins
	payload.LoginAgreementDocuments = loginAgreementDocumentsToDTO(settings.LoginAgreementDocuments)
	payload.CustomMenuItems = dto.ParseCustomMenuItems(settings.CustomMenuItems)
	payload.CustomEndpoints = dto.ParseCustomEndpoints(settings.CustomEndpoints)
	payload.DefaultSubscriptions = make([]dto.DefaultSubscriptionSetting, 0, len(settings.DefaultSubscriptions))
	for _, subscription := range settings.DefaultSubscriptions {
		payload.DefaultSubscriptions = append(payload.DefaultSubscriptions, dto.DefaultSubscriptionSetting{
			GroupID: subscription.GroupID, ValidityDays: subscription.ValidityDays,
		})
	}
	payload.AccountQuotaNotifyEmails = dto.NotifyEmailEntriesFromService(settings.AccountQuotaNotifyEmails)
	payload.OpsMonitoringEnabled = settings.OpsMonitoringEnabled
	if h.opsService != nil {
		enabled, healthy, available := h.opsService.EffectiveMonitoringState(ctx)
		payload.OpsMonitoringEnabled = available && healthy && enabled
	}

	paymentConfig := &service.PaymentConfig{}
	if h.paymentConfigService != nil {
		paymentConfig, err = h.paymentConfigService.GetPaymentConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("get payment config: %w", err)
		}
		if paymentConfig == nil {
			paymentConfig = &service.PaymentConfig{}
		}
	}
	paymentSource := reflect.ValueOf(paymentConfig).Elem()
	paymentTarget := reflect.ValueOf(&payload).Elem()
	paymentFields := map[string]string{
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
	}
	for targetField, sourceField := range paymentFields {
		paymentTarget.FieldByName(targetField).Set(paymentSource.FieldByName(sourceField))
	}

	fastPolicy, err := h.settingService.GetOpenAIFastPolicySettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("get OpenAI fast policy: %w", err)
	}
	payload.OpenAIFastPolicySettings = openaiFastPolicySettingsToDTO(fastPolicy)
	payload.DefaultPlatformQuotas, err = h.settingService.GetDefaultPlatformQuotas(ctx)
	if err != nil {
		return nil, fmt.Errorf("get default platform quotas: %w", err)
	}
	return systemSettingsResponseData(payload, authSourceDefaults), nil
}

func (h *SettingHandler) writeSystemSettingsProjection(c *gin.Context) {
	projection, err := h.SystemSettingsProjection(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, projection)
}
