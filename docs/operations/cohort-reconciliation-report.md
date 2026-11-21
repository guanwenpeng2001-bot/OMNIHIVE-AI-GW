# 存量八类数据对账报告（本地只读实例）

> 本报告仅记录 **sanitized 的计数与余额**，不包含任何 API Key、上游密钥、DSN、票据或客户隐私数据。
> 对账对象：`http://127.0.0.1:18080` 对应的 PostgreSQL `127.0.0.1:55432`，数据库 `omnihive`。
> 对账时间：2026-11-20（Asia/Shanghai）。
> 口径：单次可重复读快照；软删除/留存记录按表各自口径计数；金额为 8 位十进制字符串。

## 结论摘要

- 8 类业务对象均已在本实例完成 **本地只读对账**，逐类给出基数与余额，无密钥材料外泄。
- 钱包余额与钱包事件流水 **精确一致**：`users.balance` 合计 = `billing_wallet_events.amount` 合计 = `499.99227650` USD，`unexplained_differences = 0`。
- 活跃 API Key 与当前凭据一一对应：2 个活跃 Key、2 个 `current` 凭据、2 个 `revoked` 历史凭据。
- 本报告是 **I04 的本地可复验证据**；它 **不是** 生产 8-cohort 截止时点对账（`I04` 生产签核仍需在真实客户生产环境按相同 cutoff 复跑）。

## 一、八类对账基表与本地计数

| # | Cohort | 计数口径 | 本实例结果 |
| ---: | --- | --- | --- |
| 1 | users | `SELECT count(*) FROM users WHERE deleted_at IS NULL` | 1 |
| 2 | credentials / accounts | `accounts` 活跃上游账号；`credential_secrets` 引用；`provider_connections`；`connection_credential_versions` | 5 / 3 / 5 / 4 |
| 3 | api_keys | `api_keys` 活跃行；`api_key_credentials` 当前/撤销凭据 | 2 live；4 credentials（2 current + 2 revoked） |
| 4 | balances / wallets | `billing_funding_accounts`；`billing_wallet_events`；`users.balance` | 1 funding account；34 wallet events |
| 5 | orders | `payment_orders`、`payment_order_history`、`payment_commands` | 0 / 0 / 0 |
| 6 | tasks / jobs | `upstream_jobs`、`batch_image_jobs`、`image_tasks` | 0 / 0 / 0 |
| 7 | subscriptions | `user_subscriptions`、`subscription_plans` | 0 / 0 |
| 8 | audit_logs | `audit_logs` | 302 |

## 二、钱包余额对账（balance parity）

```sql
SELECT fa.user_id, fa.currency,
       u.balance                        AS user_balance,
       coalesce(sum(e.amount), 0)       AS wallet_event_sum
FROM billing_funding_accounts fa
JOIN users u ON u.id = fa.user_id
LEFT JOIN billing_wallet_events e
       ON e.funding_account_id = fa.id AND e.currency = fa.currency
GROUP BY fa.user_id, fa.currency, u.balance;
```

本实例结果：

```text
user_id | currency | user_balance  | wallet_event_sum
      1 | USD      | 499.99227650  | 499.99227650
```

按事件类型分解（`billing_wallet_events`）：

```text
kind            | rows | sum(amount)
opening         |    1 | 100.00000000
balance_change  |   33  | 399.99227650
合计             |   34  | 499.99227650
```

对账结论：`users.balance` 与钱包事件流水逐币种相等，差异为零。

## 三、Key 与凭据对账

```sql
SELECT count(*) AS live_keys FROM api_keys WHERE deleted_at IS NULL;
SELECT status, count(*) FROM api_key_credentials GROUP BY status ORDER BY status;
```

本实例结果：

```text
live_keys            | 2
api_key_credentials  | current=2, revoked=2
```

活跃 Key 数 = 当前凭据数（2 = 2）。每个活跃 Key 恰好一个 `current` 凭据；历史轮换凭据保留为 `revoked`。

## 四、CLI 对账入口

BE-23 只读 CLI 已提供实例级清单（覆盖 `workspaces/projects/api_keys/api_key_credentials/accounts/provider_connections/credential_secrets/connection_credential_versions/users/upstream_jobs/batch_image_jobs`）：

```sh
umask 077
OMNIHIVE_REFACTOR_DSN='<只读应用库 DSN>' \
  omnihivectl refactor inventory --format=json > inventory.json
OMNIHIVE_REFACTOR_DSN='<只读应用库 DSN>' \
  omnihivectl refactor status --format=json > status.json
```

> CLI 的 `inventory/status` 目前只读；`migrate --module --workspace --resume`、
> `reconcile --currency --cutoff`、`cutover --module --workspace --expected-version`
> 在 `contracts/migrations/migration/INTEGRATION.md` 中仍为 **planned**，尚未接线。
> 因此生产 `I04` 的 8-cohort 截止时点对账与单写权切换仍需在真实生产环境执行，不能由本地实例替代。

## 五、生产 Cutoff 对账步骤（待生产环境执行）

1. 记录发布版本、维护窗口、部署身份、数据库/Redis/卷绑定、备份对象存储与加密密钥连续性。
2. 静默写入者，选择统一 cutoff，读取旧/新两侧快照。
3. 对每个 cohort 比较基数、状态与版本边界；按 workspace/project/funder/currency 比较期初、holds、pending receipts、唯一 effect。
4. 逐币种校验 `期初 + 已入账 effects = 期末`，且 `unexplained_differences = 0`。
5. 记录逐 cohort 的 `rollback_floor`、例外隔离与复跑结果；未达成前 `eligible_for_switch = false`。

本报告仅证明本地实例的八类基数与余额一致；生产签核仍保留为 `I04` 阻断项。
