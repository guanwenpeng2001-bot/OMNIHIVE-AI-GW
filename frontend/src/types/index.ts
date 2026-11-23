/** Compatibility barrel. New consumers should import from their domain. */
export * from './common'
export * from './user'
export * from './admin'
export * from './model'
export type { SubscriptionPlan, PaymentOrder, CheckoutInfoResponse } from './payment'

export type {
  PlatformQuotaItem, PlatformQuotaUpdateItem, PlatformQuotaPlatform,
  PlatformQuotaWindow, PlatformQuotasResponse
} from '@/api/admin/users'
export type { AdminUpdateAccountRequest } from '@/api/admin/accounts'
