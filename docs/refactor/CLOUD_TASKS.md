# Codex Cloud 任务清单（从固定 SHA 出发）

固定基线：`codex/refactor` @ `146fc5cdb13100cc8bd2bd3b238f67f8bba53d8d`（已推送到 origin）。

Cloud 环境：`OMNIHIVE-AI-GW`。每个任务从基线新建自己的分支，只开 Draft PR 指向 `codex/refactor`；
不要 rebase 到更新的 `codex/refactor`，不要合并其他 Cloud 分支，不要 push 测试产物、数据库文件或密钥。

## 调度分组（按依赖）

| 批次 | 任务 | 依赖 | 说明 |
| --- | --- | --- | --- |
| 立即 | C1 = BE-07 定价权威与报价 | BE-02、BE-05 已合并 | 唯一精确计价器 |
| 立即 | C2 = BE-08 路由规划器与 trace | BE-01/03/05/06 已合并 | 生产与模拟共用计划器 |
| 立即 | C5 = BE-24A 发布验证骨架 | 无（计划允许提前） | OpenAPI 冻结、旅程 fixture、证据模板 |
| 条件 | C3 = BE-11 精确结算 | 需要 BE-07 | 用 C1 分支或合并后基线 |
| 条件 | C4 = BE-12 协议/供应商拆分 | 需要 BE-08 | 用 C2 分支或合并后基线 |

条件任务在同一批并行时，按 `docs/refactor/backend/API_CONTRACTS.md` 的接口实现，不合并依赖分支，
在 PR 描述里列出待协调的集成点。

质量门（`scripts/quality-gate.sh`）由本地 `QG` 工作包负责，Cloud 不重复实现。

## 验收基线命令

```sh
cd backend && go build ./... && go vet ./... && go test ./...
cd frontend && pnpm install --frozen-lockfile && pnpm run typecheck
```

需要 PostgreSQL/Redis 的集成测试：用临时容器（`postgres:16-alpine`、`redis:7-alpine`），
跑完即删；无法启动容器时在 PR 里标注 "integration pending" 并写明原因。
