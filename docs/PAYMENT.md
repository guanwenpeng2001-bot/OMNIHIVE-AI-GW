# Payment System Configuration Guide

OmniHive has a built-in payment system that enables user self-service top-up without deploying a separate payment service.

---

## Table of Contents

- [Supported Payment Methods](#supported-payment-methods)
- [Quick Start](#quick-start)
- [System Settings](#system-settings)
- [Provider Configuration](#provider-configuration)
- [Provider Instance Management](#provider-instance-management)
- [Webhook Configuration](#webhook-configuration)
- [Payment Flow](#payment-flow)
- [Subscription Plans](#subscription-plans)
- [Migrating from an External Payment Service](#migrating-from-an-external-payment-service)

---

## Supported Payment Methods

| Provider | Payment Methods | Description |
|----------|----------------|-------------|
| **EasyPay** | Alipay, WeChat Pay | Third-party aggregation via EasyPay protocol |
| **Alipay (Direct)** | Desktop QR code, mobile Alipay redirect | Direct integration with Alipay Open Platform, returning desktop QR codes and mobile WAP/app launch links |
| **WeChat Pay (Direct)** | Native QR, H5, MP/JSAPI Pay | Direct integration with WeChat Pay APIv3 with environment-aware routing |
| **Stripe** | Card, Alipay, WeChat Pay, Link, etc. | International payments, multi-currency support |
| **Airwallex** | Airwallex Payment Intent (hosted payment page) | International payments through an Airwallex account-scoped API key, multi-currency support |

> Alipay/WeChat Pay direct and EasyPay can both exist as backend provider instances, but the frontend always exposes only two visible buttons: `Alipay` and `WeChat Pay`. Admins choose exactly one source for each visible method: direct or EasyPay. Direct channels connect to payment APIs directly with lower fees; EasyPay aggregates through third-party platforms with easier setup.

> Select an EasyPay-compatible provider based on protocol compatibility, settlement currency, merchant eligibility, webhook signing and refund support. Fees and settlement schedules depend on the provider contract.

---

## Quick Start

1. Go to Admin Dashboard → **Settings** → **Payment Settings** tab
2. Enable **Payment**
3. Configure basic parameters (amount range, timeout, etc.)
4. Add at least one provider instance in **Provider Management**
5. Users can now top up from the frontend

---

## System Settings

Configure the following in Admin Dashboard **Settings → Payment Settings**:

### Basic Settings

| Setting | Description | Default |
|---------|-------------|---------|
| **Enable Payment** | Enable or disable the payment system | Off |
| **Product Name Prefix** | Prefix shown on payment page | - |
| **Product Name Suffix** | Suffix (e.g., "Credits") | - |
| **Minimum Amount** | Minimum single top-up amount | 1 |
| **Maximum Amount** | Maximum single top-up amount (empty = unlimited) | - |
| **Daily Limit** | Per-user daily cumulative limit (empty = unlimited) | - |
| **Order Timeout** | Order timeout in minutes (minimum 1) | 30 |
| **Max Pending Orders** | Maximum concurrent pending orders per user | 3 |
| **Load Balance Strategy** | Strategy for selecting provider instances | Round Robin |

### Frontend Visible Method Routing

The current payment UX keeps the frontend method list unified and does not expose provider brands directly:

- **Alipay**: when enabled, this button must be routed to either `Alipay (Direct)` or `EasyPay Alipay`
- **WeChat Pay**: when enabled, this button must be routed to either `WeChat Pay (Direct)` or `EasyPay WeChat`
- Each visible method can route to only one source at a time
- If a visible method is enabled without a selected source, the frontend will not expose that method

### Load Balance Strategies

| Strategy | Description |
|----------|-------------|
| **Round Robin** | Distribute orders to instances in rotation |
| **Least Amount** | Prefer instances with the lowest daily cumulative amount |

### Cancel Rate Limiting

Prevents users from repeatedly creating and canceling orders:

| Setting | Description |
|---------|-------------|
| **Enable Limit** | Toggle |
| **Window Mode** | Sliding / Fixed window |
| **Time Window** | Window duration |
| **Window Unit** | Minutes / Hours |
| **Max Cancels** | Maximum cancellations allowed within the window |

### Help Information

| Setting | Description |
|---------|-------------|
| **Help Image** | Customer service QR code or help image (supports upload) |
| **Help Text** | Instructions displayed on the payment page |

---

## Provider Configuration

Each provider type requires different credentials. Select the type when adding a new provider instance in **Provider Management → Add Provider**.

> **Callback URLs are auto-generated**: When adding a provider, the Notify URL and Return URL are automatically constructed from your site domain. You only need to confirm the domain is correct.

### EasyPay

Compatible with any payment service that implements the EasyPay protocol.

| Parameter | Description | Required |
|-----------|-------------|----------|
| **Merchant ID (PID)** | EasyPay merchant ID | Yes |
| **Merchant Key (PKey)** | EasyPay merchant secret key | Yes |
| **API Base URL** | EasyPay API base address | Yes |
| **Alipay Channel ID** | Specify Alipay channel (optional) | No |
| **WeChat Channel ID** | Specify WeChat channel (optional) | No |

### Alipay (Direct)

Direct integration with Alipay Open Platform. Mobile flows return an Alipay WAP/app redirect URL. Desktop flows prefer Face-to-Face Precreate QR payloads; if the merchant has not enabled that product, the provider falls back to Computer Website Pay and also returns the cashier URL so the frontend can render a QR code or open the hosted checkout page directly.

| Parameter | Description | Required |
|-----------|-------------|----------|
| **AppID** | Alipay application AppID | Yes |
| **Private Key** | RSA2 application private key | Yes |
| **Alipay Public Key** | Alipay public key | Yes |

### WeChat Pay (Direct)

Direct integration with WeChat Pay APIv3. Supports Native QR code payment, H5 payment, and MP/JSAPI payment inside the WeChat environment.

| Parameter | Description | Required |
|-----------|-------------|----------|
| **AppID** | WeChat Pay AppID | Yes |
| **Merchant ID (MchID)** | WeChat Pay merchant ID | Yes |
| **Merchant API Private Key** | Merchant API private key (PEM format) | Yes |
| **APIv3 Key** | 32-byte APIv3 key | Yes |
| **WeChat Pay Public Key** | WeChat Pay public key (PEM format) | Yes |
| **WeChat Pay Public Key ID** | WeChat Pay public key ID | Yes |
| **Certificate Serial Number** | Merchant certificate serial number | Yes |

### Stripe

International payment platform supporting multiple payment methods and currencies.

| Parameter | Description | Required |
|-----------|-------------|----------|
| **Secret Key** | Stripe secret key (`sk_live_...` or `sk_test_...`) | Yes |
| **Publishable Key** | Stripe publishable key (`pk_live_...` or `pk_test_...`) | Yes |
| **Webhook Secret** | Stripe Webhook signing secret (`whsec_...`) | Yes |

---

## Provider Instance Management

You can create **multiple instances** of the same provider type for load balancing and risk control:

- **Multi-instance load balancing** — Distribute orders via round-robin or least-amount strategy
- **Independent limits** — Each instance can have its own min/max amount and daily limit
- **Independent toggle** — Enable/disable individual instances without affecting others
- **Refund control** — Enable or disable refunds per instance
- **Payment methods** — Each instance can support a subset of payment methods
- **Ordering** — Drag to reorder instances

### Instance Limit Configuration

Each instance supports these limits:

| Limit | Description |
|-------|-------------|
| **Minimum Amount** | Minimum order amount accepted by this instance |
| **Maximum Amount** | Maximum order amount accepted by this instance |
| **Daily Limit** | Daily cumulative transaction limit for this instance |

> During load balancing, instances that exceed their limits are automatically skipped.

---

## Webhook Configuration

Payment callbacks are essential for the payment system to work correctly.

### Callback URL Format

When adding a provider, the system auto-generates callback URLs from your site domain:

| Provider | Callback Path |
|----------|-------------|
| **EasyPay** | `https://your-domain.com/api/v1/payment/webhook/easypay` |
| **Alipay (Direct)** | `https://your-domain.com/api/v1/payment/webhook/alipay` |
| **WeChat Pay (Direct)** | `https://your-domain.com/api/v1/payment/webhook/wxpay` |
| **Stripe** | `https://your-domain.com/api/v1/payment/webhook/stripe` |
| **Airwallex** | `https://your-domain.com/api/v1/payment/webhook/airwallex` |

> Replace `your-domain.com` with your actual domain. For EasyPay / Alipay / WeChat Pay, the callback URL is auto-filled when adding the provider — no manual configuration needed. Stripe and Airwallex webhook endpoints are created in the provider's own dashboard.

### Stripe Webhook Setup

1. Log in to [Stripe Dashboard](https://dashboard.stripe.com/)
2. Go to **Developers → Webhooks**
3. Add an endpoint with the callback URL
4. Subscribe to events: `payment_intent.succeeded`, `payment_intent.payment_failed`
5. Copy the generated Webhook Secret (`whsec_...`) to your provider configuration

### Airwallex Webhook Setup

1. Log in to the [Airwallex web app](https://www.airwallex.com/)
2. Create an endpoint pointing at the callback URL above
3. Subscribe to **Payment Intent → Succeeded** (`payment_intent.succeeded`); subscribing to **Payment Intent → Cancelled** (`payment_intent.cancelled`) is recommended so cancelled payments are closed out promptly
4. Keep the account default (or latest stable) API version on the endpoint
5. Copy the Webhook Secret into the provider's `webhookSecret` field

Airwallex provider fields: `clientId`, `apiKey`, `webhookSecret`, `apiBase`, `accountId`, `countryCode`, `currency`. `apiBase` must match the key environment — `https://api-demo.airwallex.com/api/v1` for sandbox keys and `https://api.airwallex.com/api/v1` for production keys; mixing them fails with `credentials_invalid`. Leave `accountId` empty unless you use multiple accounts, an organization-level key, or connected-account payments. The scoped API key needs **Read and Write** under account-level *Payment Acceptance*; do not grant Spend, Payouts, Transfers, Funds Splits or POS Terminal permissions unless you actually need them.

### Important Notes

- Callback URLs must use **HTTPS** (required by Stripe, strongly recommended for others)
- Ensure your firewall allows callback requests from payment platforms
- The system automatically verifies callback signatures to prevent forgery
- Balance top-up is processed automatically upon successful payment — no manual intervention needed

---

## Payment Flow

```
User selects amount and payment method
       │
       ▼
  Create Order (PENDING)
  ├─ Validate amount range, pending order count, daily limit
  ├─ Load balance to select provider instance
  └─ Call provider to get payment info
       │
       ▼
  User completes payment
  ├─ EasyPay     → QR code / H5 redirect
  ├─ Alipay      → Desktop QR payload (Face-to-Face preferred, Website Pay fallback) / mobile Alipay redirect
  ├─ WeChat Pay  → Desktop Native QR / non-WeChat H5 / in-WeChat JSAPI
  └─ Stripe      → Payment Element (card/Alipay/WeChat/etc.)
       │
       ▼
  Webhook callback verified → Order PAID
       │
       ▼
  Auto top-up to user balance → Order COMPLETED
```

### Order Status Reference

| Status | Description |
|--------|-------------|
| `PENDING` | Waiting for user to complete payment |
| `PAID` | Payment confirmed, awaiting balance credit |
| `COMPLETED` | Balance credited successfully |
| `EXPIRED` | Timed out without payment |
| `CANCELLED` | Cancelled by user |
| `FAILED` | Balance credit failed, admin can retry |
| `REFUND_REQUESTED` | Refund requested |
| `REFUNDING` | Refund in progress |
| `REFUNDED` | Refund completed |

### Timeout and Fallback

- Before marking an order as expired, the background job queries the upstream payment status first
- If the user has actually paid but the callback was delayed, the system will reconcile automatically
- The background job runs every 60 seconds to check for timed-out orders

---

## Subscription Plans

Subscription plans sell time-boxed access to one group instead of a balance top-up. They are part of the built-in payment system:

- **Admin**: `GET/POST /api/v1/admin/payment/plans` and `PUT/DELETE /api/v1/admin/payment/plans/:id` manage plans. A plan requires a name, a group, a price greater than zero, and a validity period (`validity_days` plus `validity_unit`); an optional `original_price` is display-only.
- **User**: `GET /api/v1/payment/plans` lists the plans a user can buy; checkout creates an order with `order_type: subscription` (`balance` is the top-up type).
- **Fulfillment**: the paid order grants the bound group for the configured validity period. Requests inside that group are still routed, quota-checked and billed against the concrete provider platform (for a Composite group, against the resolved provider).

Plans bind to a group; when that group is a Composite group the plan grants Composite access only, and per-request resolution still applies.

---

## Migrating from an External Payment Service

If you previously used a separate payment service, evaluate the following integration differences before migrating to built-in payment:

### Key Differences

| Aspect | External Payment Service | Built-in Payment |
|--------|-----------|-----------------|
| Deployment | Separate service and dependencies | Built into OmniHive, no extra deployment |
| Payment Methods | Depends on the external service | EasyPay, Alipay, WeChat, Stripe, Airwallex |
| Configuration | External service configuration | Unified in OmniHive admin dashboard |
| Top-up Integration | Typically via Admin API callback | Internal processing |
| Subscription Plans | Depends on the external service | Supported (see [Subscription Plans](#subscription-plans)) |
| Order Management | Separate admin interface | Integrated in OmniHive admin dashboard |

### Migration Steps

1. Enable payment in OmniHive admin dashboard and configure supported providers with the appropriate payment credentials
2. Update webhook callback URLs to OmniHive's callback endpoints
3. Verify that new orders are processed correctly via built-in payment
4. Decommission the external payment service after reconciling pending orders

> **Note**: Historical orders from the external service are not automatically migrated. Retain access to historical records for reconciliation.
