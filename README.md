<p align="center">
  <img src="assets/logo.svg" alt="OmniHive Logo" width="112" />
</p>

# OmniHive AI Gateway (OMNIHIVE-AI-GW)

现有部署升级前请参阅[部署身份迁移说明](docs/operations/identity-migration.md)。

<p align="center"><strong>OmniHive — Unified AI Inference Gateway</strong></p>
<p align="center"><strong>「智能算力中转蜂巢 · 企业级多模态 AI 统一网关与治理中枢」</strong></p>
<p align="center">
  <img src="https://img.shields.io/badge/Go-1.27-00ADD8?logo=go" alt="Go 1.27" />
  <img src="https://img.shields.io/badge/Vue_3-TypeScript-42b883?logo=vuedotjs" alt="Vue 3 + TypeScript" />
  <img src="https://img.shields.io/badge/PostgreSQL-18-4169E1?logo=postgresql" alt="PostgreSQL" />
  <img src="https://img.shields.io/badge/Redis-8-DC382D?logo=redis" alt="Redis" />
  <img src="https://img.shields.io/badge/Docker-Compose-2496ED?logo=docker" alt="Docker" />
  <img src="https://img.shields.io/badge/Native-OpenAI%20%2F%20Anthropic%20%2F%20Gemini%20%2F%20Codex-f59e0b" alt="OpenAI / Anthropic / Gemini / Codex Native" />
</p>

[简体中文](README.md) | [English](README_EN.md)

## 项目定位与核心特性

**OmniHive — Unified AI Inference Gateway（多协议 AI 推理统一网关）**面向需要统一管理多供应商、多账号和多模态 AI 调用的团队，提供协议入口、路由治理、精确结算与运维控制台。仓库标识为 `OMNIHIVE-AI-GW`，项目地址为 [GitHub](https://github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW)。

- **多协议线缆兼容网关**：以 wire-compatible 方式对外提供 OpenAI `/v1/chat/completions`、`/v1/responses`、`/v1/messages` 与 Google Gemini `v1beta` 等协议入口，并原生桥接 Anthropic Messages、Codex CLI、DashScope、Volcengine、Kaggle 多模态通道。实际可用端点取决于平台、账号类型、模型白名单和上游授权，详见[协议矩阵](docs/gateway-platform-protocol-matrix.md)。
- **五步上游连接向导与出口资源池**：连接向导引导「选择平台 → 填写凭据 → 网络/鉴权验证 → 模型预览 → 激活」，凭证与账号健康度统一纳入 Egress Resource Pools，配合粘性会话与代理组出口管理多账号池。
- **路由策略工作台与路由解释**：按渠道能力配置 ZDR、自动回退、提示词缓存与地域偏好；请求详情通过 Route Explain 解释命中策略、分组与上游选择依据。
- **精确十进制台账与权威报价**：Exact Decimal Ledger 以精确十进制记账，服务端 Authoritative Quotes 提供可核对的预估与结算证据，支撑用量结算、成本追踪与对账。
- **工作空间/项目 RBAC 与 BYOK**：工作空间与项目隔离的成员授权与访问控制，支持租户自带密钥（BYOK）与资金兜底。
- **公开模型广场与实时遥测状态**：Public Model Plaza 展示可公开的模型、价格与详情；Live Telemetry Status 暴露公开状态与事件生命周期。
- **请求瀑布与异步媒体任务中心**：Request Waterfall 呈现请求逐阶段耗时；异步媒体任务以队列执行，支持状态追踪、失败子项重试与打包下载。
- **`.ohplugin` 扩展系统**：`.ohplugin` 为签名、沙箱隔离运行的扩展插件包；历史 `.s2plugin` 包仅在迁移兼容窗口内验签导入。`prompt-guard`、`rate-limiter`、`cache-layer`、`cost-tracker` 四大内置网关扩展承担提示词治理、限流、缓存与成本追踪。
- **面板版本检查与一键更新/回滚**：管理员通过左上角版本徽标检查 GitHub Release、执行更新或选择允许回滚的历史版本。

## 系统架构概览

```mermaid
flowchart TD
    Clients["客户端 SDK / CLI / Web 工作台"] --> Ingress["OmniHive Ingress · 认证与协议入口"]
    Ingress --> Engines["四大扩展治理引擎<br/>prompt-guard · rate-limiter · cache-layer · cost-tracker"]
    Engines --> Groups["分组模型白名单与协议桥接<br/>工作空间策略 · ZDR · 回退 · 缓存 · 地域"]
    Groups --> Pools["多账号池健康度调度<br/>粘性会话 · 配额探针"]
    Pools --> Egress["代理组出口与可选传输插件"]
    Egress --> Providers["OpenAI / Anthropic / Gemini / DashScope<br/>Volcengine / Kaggle / 其他已适配渠道"]
    Providers --> Ledger["异步结算台账与可观测监控"]
    Ledger --> Console["管理控制台 · 用量窗口 · 批量任务"]
```

同步请求、流式响应和异步任务共享鉴权与计费边界，但各自遵循对应协议的生命周期。PostgreSQL 保存业务数据与结算记录，Redis 支撑缓存、队列和协调；能力边界与例外见专题文档。

## 快速开始

### Docker Compose

准备 Docker Engine 与 Docker Compose 2.24+。以下命令使用本地构建镜像：

```bash
git clone 'https://github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW.git'
cd OMNIHIVE-AI-GW
cp deploy/.env.example deploy/.env
```

编辑 `deploy/.env`，设置数据库密码、管理员账号和持久化密钥，然后构建与启动：

```bash
docker build -t 'ghcr.io/guanwenpeng2001-bot/omnihive-ai-gw:latest' .
docker compose --project-directory deploy -f deploy/docker-compose.yml config --quiet
docker compose --project-directory deploy -f deploy/docker-compose.yml up -d
docker compose --project-directory deploy -f deploy/docker-compose.yml logs -f omnihive
```

访问 `http://localhost:8080` 完成初始化。仓库 owner 占位符需由部署环境解析为实际值，镜像地址也需按实际 GHCR 命名空间配置。已有发布镜像时可省略本地构建，先执行相同 Compose 配置的 `pull` 命令。

| 环境变量 | 用途 |
| --- | --- |
| `POSTGRES_PASSWORD` | Compose 内置 PostgreSQL 密码，部署前必须设置 |
| `JWT_SECRET` | 固定的登录令牌签名密钥，重建容器时保持不变 |
| `TOTP_ENCRYPTION_KEY` | 固定的双因素认证加密密钥，备份时一并保存 |
| `ADMIN_EMAIL` / `ADMIN_PASSWORD` | 初始管理员配置 |
| `BIND_HOST` / `SERVER_PORT` | 主机监听地址与发布端口；Compose 容器内部仍使用 8080 |
| `UPDATE_GITHUB_TOKEN` | 可选，用于访问私有仓库的 Release 元数据与资产 |
| `DATABASE_*` / `REDIS_*` | 独立部署连接参数，参见环境模板 |

用 `openssl rand -hex 32` 分别生成签名与加密密钥，并保护 `.env`。保留 `/app/data`、PostgreSQL 与 Redis 持久化数据。外部数据库使用 `deploy/docker-compose.standalone.yml`，本地目录存储使用 `deploy/docker-compose.local.yml`。详见[部署指南](deploy/README.md)与[镜像说明](deploy/DOCKER.md)。

### 源码编译

工具链以 `backend/go.mod` 和 `frontend/package.json` 为准：Go 1.27.4、Node.js 24、pnpm 11。先构建前端，再嵌入后端：

```bash
pnpm --dir frontend install --frozen-lockfile
pnpm --dir frontend build
cd backend
go test -tags=unit ./...
go build -tags embed -o ../omnihive ./cmd/server
cd ..
```

准备 PostgreSQL / Redis，并按 `deploy/config.example.yaml` 配置服务连接；可将配置放到运行目录的 `config.yaml`。运行 `./omnihive`，详见[开发指南](DEV_GUIDE.md)。

### 二进制部署

从 [Releases](https://github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/releases) 下载目标系统与架构的完整发行包和 `checksums.txt`，核对 SHA-256 后解压。也可先下载并审阅安装脚本：

```bash
curl -fL --globoff 'https://raw.githubusercontent.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/main/deploy/install.sh' -o install.sh
less install.sh
sudo bash install.sh
```

安装器保留既有服务名与二进制路径，以兼容已部署实例。使用私有仓库时，通过受保护的运行环境传入 `UPDATE_GITHUB_TOKEN`。安装细节、服务管理与备份流程见[部署指南](deploy/README.md)。

## 自动发布与面板一键更新

推送 `v*` 标签会触发 GitHub Actions Release 工作流；完整发布流程构建发行资产、校验清单和镜像。仅镜像的简化发布不提供面板自更新所需的完整二进制资产。

在管理后台左上角点击版本号，检查 Release 并选择更新；发布资产携带 `checksums.txt` 时，更新器执行 SHA-256 校验，再替换二进制。更新后通过受监督的进程重启恢复服务；容器应配置重启策略，原生部署应由 systemd 等进程管理器负责拉起。重启可能中断在途请求，安排维护窗口并检查 `/health` 与 `/ready`。可从面板选择允许回滚的历史版本；二进制回滚不会撤销数据库迁移，因此升级前应备份数据库与持久化密钥。

私有仓库配置 `UPDATE_GITHUB_TOKEN`，并确认该令牌拥有目标仓库的读取权限。发布与回滚行为还取决于实际工作流、资产命名和运行权限；详见[开发指南](DEV_GUIDE.md)。

### 版本检查与命令行升级/回滚

原生二进制安装可直接检查版本并按标签升级或回滚，安装器会保留既有服务名与数据路径：

```bash
/opt/omnihive/omnihive --version            # 查看当前二进制版本
sudo bash deploy/install.sh list-versions   # 列出可用发布版本
sudo bash deploy/install.sh upgrade         # 升级到最新版本
sudo bash deploy/install.sh rollback v0.2.23            # 回滚到指定历史版本
```

Docker 部署建议固定镜像标签，升级执行 `docker compose pull` 后 `docker compose up -d`，回滚时将标签改回上一版本再重新拉起；升级前备份数据库与持久化密钥，因为二进制/镜像回滚不会撤销已应用的数据库迁移。健康检查使用 `/health`（存活）与 `/ready`（依赖就绪/排空）。

## 质量门禁

仓库提供一键质量门禁，默认执行全部阶段，也可只运行指定阶段：

```bash
scripts/quality-gate.sh                 # 全部阶段
scripts/quality-gate.sh backend         # 后端 build/vet/test
scripts/quality-gate.sh contracts generated-drift independence
```

各阶段含义、前置条件与 `--help` 用法见 `scripts/quality-gate.sh`。CI 会执行后端/前端/PostgreSQL 矩阵/安全扫描与质量门禁，详见 `.github/workflows/`。

## 目录结构与开发指南

```text
OMNIHIVE-AI-GW/
├── assets/                 # OmniHive 品牌资源
├── backend/                # Go 服务、Ent 数据模型与迁移
├── frontend/               # Vue 3 + TypeScript 管理与用户界面
├── deploy/                 # Compose、安装器与配置模板
├── docs/                   # 协议、业务能力与运维专题
└── DEV_GUIDE.md             # 开发环境、测试与贡献约定
```

| 文档 | 内容 |
| --- | --- |
| [开发指南](DEV_GUIDE.md) | 本地运行、构建、迁移、插件与代码约定 |
| [使用指南](docs/usage-guide.md) | API Key、客户端配置与调用示例 |
| [渠道配置](docs/provider-configuration.md) | 供应商与账号接入 |
| [协议矩阵](docs/gateway-platform-protocol-matrix.md) | 平台、模型和端点能力边界 |
| [组合分组](docs/COMPOSITE_GROUPS.md) | 分组组合与路由规则 |
| [批量生图](docs/BATCH_IMAGE_MVP.md) / [异步图像任务](docs/ASYNC_IMAGE_TASKS.md) | 队列、任务、重试与下载 |
| [插件开发](contracts/plugins/README.md) | `.ohplugin` v2 清单、签名、WASI 运行时与迁移 |
| [支付配置](docs/PAYMENT_CN.md) | 中立的支付渠道接入与回调配置 |
| [部署与运营说明](docs/legal/admin-compliance.zh.md) | 实例管理者责任说明 |

许可证见 [LICENSE](LICENSE)。
