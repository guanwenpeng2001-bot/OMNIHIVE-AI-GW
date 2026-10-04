package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// CompatiblePlan names an operator-selected product on an existing transport.
// Query only gates PAYG metadata; a selection never establishes entitlement.
type CompatiblePlan struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	LabelZh string `json:"label_zh"`
	Query   string `json:"query"`
}

var compatiblePlanID = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

func validateCompatiblePlans(provider CompatibleProvider) error {
	if len(provider.Plans) > 0 && len(provider.Modes) != 1 {
		return fmt.Errorf("plans require exactly one transport mode")
	}
	seen := map[string]bool{}
	for _, plan := range provider.Plans {
		if !compatiblePlanID.MatchString(plan.ID) || seen[plan.ID] {
			return fmt.Errorf("invalid or duplicate plan id %q", plan.ID)
		}
		seen[plan.ID] = true
		if strings.TrimSpace(plan.Label) == "" || strings.TrimSpace(plan.LabelZh) == "" {
			return fmt.Errorf("plan labels are required")
		}
		if plan.Query != "payg" && plan.Query != "plan" {
			return fmt.Errorf("invalid plan query")
		}
	}
	return nil
}

// ValidateBillingPlan accepts unset/clear markers, but never coerces plan IDs.
// Callers normalize explicit clear markers when writing the resulting map.
func (provider CompatibleProvider) ValidateBillingPlan(credentials map[string]any) error {
	value, present := credentials["billing_plan"]
	if !present || value == nil {
		return nil
	}
	id, ok := value.(string)
	if !ok {
		return fmt.Errorf("billing_plan must be a string or null")
	}
	if id == "" {
		return nil
	}
	for _, plan := range provider.Plans {
		if plan.ID == id {
			return nil
		}
	}
	return fmt.Errorf("unsupported compatible provider billing_plan")
}

// ClearEmptyBillingPlan removes only an explicit plan-clear marker.
func ClearEmptyBillingPlan(credentials map[string]any) {
	value, present := credentials["billing_plan"]
	if present && (value == nil || value == "") {
		delete(credentials, "billing_plan")
	}
}
