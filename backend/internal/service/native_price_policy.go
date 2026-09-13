package service

import "errors"

var ErrUnverifiedModelPrice = errors.New("model_pricing_unavailable")

// Official promotional free IDs, verified 2026-09-13 at
// https://commandcode.ai/docs/resources/pricing-limits . Never infer by suffix.
func commandCodeOfficialFreeModel(model string) bool {
	switch model {
	case "inclusionai/ling-3.0-flash-sante:free", "meituan/LongCat-2.0:free", "poolside/laguna-s-2.1-free":
		return true
	}
	return false
}
