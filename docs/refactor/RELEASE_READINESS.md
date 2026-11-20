# OmniHive 发布就绪验收报告

> 基准：`codex/refactor` · 2026-11-20
> 结论：**RELEASE READY (GO — `dev` 合入签核通过)**
> 本文记录从最初重构要求到后续全部追加要求（含领域化命名统一、去除 `BE-xx` / `WPx` / `V1` / `V2` 开发痕迹、四大家族真实上游端到端冒烟、独立 Docker 与本机双环境部署）的完整闭环状态。

## 1. 核心验收标准达成情况

1. **可复现部署（DONE）**：笔记本独立 Docker Compose 实例 `omnihive-refactor-qa`（`:18090`）与本机原生实例（`:18080`）均运行 `OmniHive v2.0.0-rc.1 (BuildType=release)`，`/health`、`/ready`、PostgreSQL 与 Redis 全部健康，迁移链直达 `320`。
2. **四大家族真实上游（DONE）**：经 Magpie TLS 网关完成真实连接创建、凭据加密存储、网络/鉴权/模型发现探测（发现 275+ 模型）、差异预览与激活；`codex/gpt-6.2-astra`、`auto/gemini-3.8-flash`、`deepseek/deepseek-v4-pro`、`auto/kimi-k3` 四条真实 `/v1/chat/completions` 请求全部 200 返回。
3. **开发者全链路闭环（DONE）**：登录 → 公开模型广场 `/model-plaza`（真实每百万 Token 价格投影） → 公开定价 `/pricing` → 创建绑定项目的 API Key → `/v1/models` → 权威服务端报价 `POST /quotes` → Playground 调试台 → 请求瀑布 `/requests` → 精确十进制台账 `/billing`（实时结算落账） → 自带密钥 `/byok`。
4. **管理员全链路闭环（DONE）**：五步中文连接向导（含 `header_overrides` 自定义请求头覆写） → 出站资源池 → 分组/渠道定价 → 模型广场修订确认 → 路由工作台与解释 → 成员与权限 `GET/PUT`（`ETag`/`If-Match` 乐观并发） → 渠道监控（主动拨测 / 流量遥测） → `.ohplugin` 扩展管理 → 项目审计与保留策略 → 工作空间遥测导出与模型目录兼容性核验工作台。
5. **领域化命名与无开发痕迹（DONE）**：
   - 后端 `internal/legacy/{identity,connections,catalog,execution,restore}`、OpenAPI `backend/api/openapi/fragments/*.yaml`、契约目录 `contracts/{api,migrations}/*`、遥测与中继服务 `channel_telemetry*` / `openai_ws_relay*` 全部改为标准领域命名，零 `BE-xx` 目录残留。
   - 前端 `costLedger`、`channel-telemetry`、`ChannelProbeStatusView`、`ChannelTelemetryStatusView`、`controlClient`、`generated/openapi.d.ts` 及纯领域 `*Api` 全部就位，界面彻底移除 `BE-*`、`WP*`、`V1 主动探测 / V2 被动监控` 等工程代号，全站控制台零报错（`consoleErrors: []`）。
6. **版本检查与升级回滚（DONE）**：顶栏版本徽标规范展示 `v2.0.0-rc.1`，支持实时检查 GitHub Release、更新提示与历史版本回滚；`deploy/install.sh`、`deploy/docker-deploy.sh`、GoReleaser 与 Release Workflow 全部对齐 Go 1.27.4。

## 2. P0 与 P1 阻断项修复核销清单

- [x] **公开模型广场与公开定价改接真实目录**：`/model-plaza` 与 `/pricing` 实时投影已发布模型与修正后的百万 Token 单价（修复原适配器百万倍缩放偏差）。
- [x] **自带密钥（BYOK）完整工作台上线**：覆盖工作空间与项目双作用域凭据托管、供应商类别校验、在线探测、`If-Match` 策略更新、轮换撤销、成本试算与事件审计。
- [x] **插件扩展页改接真实后端契约**：对接 `GET /api/v2/admin/extensions`，移除硬编码假模块回退，统一 `.ohplugin` 包格式说明。
- [x] **Chat Completions 接入执行日志与项目账单归属**：4 条 Chat/Messages 网关入口完整写入 `gateway_executions`，新增迁移 `320` 幂等回填项目密钥与历史账单归属，真实请求后费用页实时准确汇总。
- [x] **旧账号编辑与新连接并存**：管理端更新受管账号分组或请求头不再触发连接 404 失联。
- [x] **成员列表与授权版本读取补齐**：新增 `GET /api/v2/workspaces/{w}/members` 与 `GET .../members/{id}/grants`（返回 `ETag`），消除前端盲写版本号冲突。
- [x] **权威服务端报价与目录核验接线**：`POST /api/v2/workspaces/{w}/projects/{p}/quotes` 与 `/api/v2/admin/catalog/verification*` 完整挂载进路由与后台队列，Playground 优先采用服务端精确报价。
- [x] **GPT-6.2 与 Kimi 真实转发修复**：透传模型名与合法 `reasoning_effort`，对默认脚本 UA 自动替换为合规出站标识并开放账号级 `header_overrides`，四大家族模型实测全绿。
- [x] **新安装体验修复**：`setup.go` 初始化新库自动开启模型广场/注册并写入 `history_known_empty_before` 空历史起点，彻底消除新装仪表盘 `409 USAGE_HISTORY_INCOMPLETE`；新手引导仅在仪表盘轻量提示且随时可跳过。
- [x] **全站中文化与响应式宽屏**：1560px 宽屏自适应布局，补齐连接向导、出站资源池、成员权限、调试台、任务中心、审计与运维面板全部中文文案；实测截图归档于 `docs/refactor/design/screens/release-rc1/`。

## 3. 自动化门禁与实测证据

- **前端全量门禁（Codespace）**：`check:i18n` PASS、`vue-tsc --noEmit` PASS、`eslint` PASS、Vitest **476 个测试文件 / 5,404 项测试 PASS**、Vite 生产构建 PASS。
- **后端与契约门禁（Laptop + Local）**：`wire` / OpenAPI `generated-drift` PASS、`contracts` PASS、`independence` PASS、改动包 Go 单测与集成编译全部 PASS。
- **双环境真实上游冒烟（`scripts/smoke/qa_real_smoke.py`）**：
  - 笔记本 Docker（`http://localhost:18090`）：**25 executed, 0 failed**
  - 本机原生实例（`http://127.0.0.1:18080`）：**25 executed, 0 failed**
- **浏览器多视口实测截图**：`docs/refactor/design/screens/release-rc1/`（18 张 1920px 宽屏与 360px 移动端实测截图，全页面 `consoleErrors: []`）。

## 4. 生产上线前外部环境项（非代码缺陷）

- 生产支付商户密钥与公网 Webhook 回调域名配置。
- 生产域名 TLS 证书、`trusted_proxies`、对象存储备份桶及正式 GitHub Release Tag 签发（`main` 分支按约定保持不动，待 `dev` 验收后择机发版）。
