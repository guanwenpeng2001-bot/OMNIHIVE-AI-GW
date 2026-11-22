package service

import (
	"context"
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
)

// AuthorizePricingModel reuses the gateway's committed model policy (including
// named sets, exclusions, group rules and stable IDs). Quotes must not recreate
// those rules with a second SQL interpretation.
func (s *APIKeyService) AuthorizePricingModel(ctx context.Context, keyID int64, provider, model string) error {
	if s == nil || keyID <= 0 || provider == "" || model == "" {
		return fault.Forbidden
	}
	if _, ok := s.apiKeyRepo.(ModelAccessPolicyReader); !ok {
		return fault.Forbidden
	}
	ctx, err := s.AttachModelAccessPolicy(ctx, keyID)
	if err != nil {
		return err
	}
	ctx = WithProjectKeyModelPolicy(ctx, provider)
	if !visibleModelPolicyAllows(ctx, model) {
		return fault.Forbidden
	}
	return nil
}
