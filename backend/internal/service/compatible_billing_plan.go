package service

import "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/domain"

// PaygBalanceAllowed gates commercial plans only. Callers must still enforce
// their existing platform, transport mode, origin and credential checks.
func PaygBalanceAllowed(platform string, credentials map[string]any) (bool, error) {
	provider, _ := domain.LookupCompatibleProvider(platform)
	if err := provider.ValidateBillingPlan(credentials); err != nil {
		return false, err
	}
	selected, _ := credentials["billing_plan"].(string)
	for _, plan := range provider.Plans {
		if plan.ID == selected {
			return plan.Query == "payg", nil
		}
	}
	return true, nil
}
