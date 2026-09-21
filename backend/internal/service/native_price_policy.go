package service

import "errors"

var ErrUnverifiedModelPrice = errors.New("model_pricing_unavailable")

// Official promotional free IDs, verified 2026-09-13 and re-checked 2026-09-21 at
// https://commandcode.ai/docs/resources/pricing-limits . Never infer by suffix.
//
// 已登记的证据边界（交协调者裁决，不自行改价格口径）：2026-09-21 抓取的定价页
// 只把 Laguna S 2.1 / Ling 3.0 Flash Sante 标为 Free，并把 LongCat 2.0 标为
// $0.30/$1.20；catalog 里也只有 `inclusionai/ling-3.0-flash-sante:free` 与
// `poolside/laguna-s-2.1-free` 带免费后缀，`meituan/LongCat-2.0` 没有。
// 因此 `meituan/LongCat-2.0:free` 这条只对历史上带后缀的别名生效，
// 不会把 catalog 的付费 ID 判成免费（无后缀匹配）。
func commandCodeOfficialFreeModel(model string) bool {
	switch model {
	case "inclusionai/ling-3.0-flash-sante:free", "meituan/LongCat-2.0:free", "poolside/laguna-s-2.1-free":
		return true
	}
	return false
}
