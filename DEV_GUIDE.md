# OmniHive AI Gateway 开发指南

本指南面向 `OMNIHIVE-AI-GW` 开发者。项目仓库：[OmniHive AI Gateway](https://github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW)。

## 环境与目录

| 工具 | 项目约定 |
| --- | --- |
| Go | 1.27.0，以 `backend/go.mod` 为准 |
| Node.js / pnpm | Node.js 24、pnpm 11，以 `frontend/package.json` 为准 |
| PostgreSQL / Redis | Compose 默认 PostgreSQL 18、Redis 8 |
| Docker Compose | 2.24+，支持配置中的可选 `env_file` |

`backend/cmd/server` 是服务入口；`backend/internal/handler`、`service`、`repository` 分别负责请求处理、业务规则与数据访问。`backend/ent/schema` 定义实体，`backend/migrations` 保存 SQL 迁移。前端的 `api`、`stores`、`views`、`components` 分别承载请求、共享状态、页面与复用组件。

## 本地运行

从仓库根目录安装依赖并启动前端：

```bash
pnpm --dir frontend install --frozen-lockfile
pnpm --dir frontend dev
```

准备 PostgreSQL 和 Redis，复制 `deploy/config.example.yaml` 到本地运行目录的 `config.yaml` 并填写连接信息。也可使用实际支持的 `DATABASE_*`、`REDIS_*` 环境变量。不要把 Compose 的 `POSTGRES_PASSWORD` 误当作原生进程的 `DATABASE_PASSWORD`。设置固定的 `JWT_SECRET` 和 `TOTP_ENCRYPTION_KEY`。

```bash
cd backend
go run ./cmd/server
```

前端代理配置见 `frontend/vite.config.ts`；前后端地址不一致时按该文件支持的环境配置调整。不要提交本地数据库凭据、令牌或 `.env`。

## 构建与验证

先构建前端，Vite 将静态资源输出到后端嵌入目录；再编译服务：

```bash
pnpm --dir frontend build
cd backend
go build -tags embed -o ../omnihive ./cmd/server
go test -tags=unit ./...
```

后端集成测试需按各测试夹具要求准备隔离依赖：

```bash
go test -tags=integration ./...
```

前端验证从仓库根目录执行：

```bash
pnpm --dir frontend test:run
pnpm --dir frontend typecheck
pnpm --dir frontend lint:check
pnpm --dir frontend exec vitest run src/views/admin/__tests__/SettingsView.spec.ts src/api/__tests__/admin.system.rollback.spec.ts
```

临时文件与测试缓存放在仓库 `.pi/` 下的任务专用目录；Windows 设置 `TEMP`、`TMP`，POSIX 设置 `TMPDIR`。涉及数据库的测试使用独立实例或独立数据库，避免连接生产数据。

## 数据库迁移

启动时迁移器应用 SQL 并记录 SHA-256 校验和。已经应用的迁移不可改写；修正行为应新增顺序编号的 `NNN_description.sql`。普通迁移按事务执行，只写向前迁移，不要把可执行的回滚 SQL 混在同一文件中。

并发索引使用 `NNN_description_notx.sql`，仅包含幂等的 `CREATE INDEX CONCURRENTLY IF NOT EXISTS` 或 `DROP INDEX CONCURRENTLY IF EXISTS` 语句。修改 Ent schema 后，在 `backend` 中执行：

```bash
go generate ./ent
```

提交相应生成代码及新迁移，并验证空库启动、已有数据库升级和相关读写路径。升级前备份；二进制回滚不能逆转数据库结构。详细规则见 [迁移说明](backend/migrations/README.md)。

## 插件与网关扩展

`.s2plugin` 包将独立进程、清单、签名和可选静态 UI 一起交付。宿主以受约束的 gRPC 能力协议调用插件，通过 iframe 与 UI Bridge 提供管理界面。传输插件目前的公开能力示例是 `openai.oauth.outbound_transport.v1`；声明额外能力不会自动产生路由。配置由宿主加密保存，发布前校验包签名、兼容版本与真实运行时。

内置扩展与外部传输插件有不同职责：

| 扩展 | 主要职责 |
| --- | --- |
| `prompt-guard` | 提示词策略与审查 |
| `rate-limiter` | 请求速率与资源约束 |
| `cache-layer` | 可缓存请求与缓存策略 |
| `cost-tracker` | 调用成本追踪 |

新增能力须维护认证、账号选择、取消、超时、流式结束和结算幂等边界。避免由插件绕过模型白名单、代理出口约束或宿主结算流程。协议、打包与验收细节见 [插件开发教程](docs/PLUGIN_DEVELOPMENT.md)。

## 代码约定

- Go 代码使用 `gofmt`，遵循现有分层；跨层错误应保留可判断的类型和上下文，日志不得记录凭据。
- Vue 使用 Composition API 与 TypeScript；API 请求集中在 `src/api`，新增文案同步中英文语言资源，复用既有组件与设计变量。
- 依赖由 pnpm 管理，新增或变更依赖时同步锁文件；仅修改包名称无需重建依赖树。
- 保持现有 Go module / import 路径、容器用户、二进制位置、环境变量键和协议字面量兼容。品牌名更新不能改变运行时契约。
- `.sh` 文件使用 UTF-8 与 LF；配置默认值应与后端一致，示例使用占位凭据。
- 修改公共接口时同步调用方、测试替身与契约用例；优先验证行为，避免只断言实现细节。

## 发布、更新与运维

`v*` 标签触发 `.github/workflows/release.yml`；完整发布提供平台发行包、`checksums.txt` 与容器镜像，简化发布仅构建其配置的镜像。面板版本徽标调用更新接口，校验可用的 SHA-256 清单、替换二进制，并通过进程监督恢复服务。回滚仅限服务允许的版本。

私有仓库使用 `UPDATE_GITHUB_TOKEN`。令牌通过安全的运行环境配置，不写入仓库或日志。容器中的 `/app` 和数据目录必须允许运行用户执行更新所需的操作；原生部署使用服务管理器。发布验证应覆盖 `/health`、`/ready`、登录、真实协议调用及计费结果。重启可能影响在途请求，数据库与密钥备份应先于升级。

## 品牌资源

`assets/logo.svg` 使用 256×256 画布、黑曜石色 `#16161a` 和暖琥珀色 `#f59e0b`。保持外六边形、内环与中心蜂巢单元的比例；四周留白至少为画布宽度的 1/16。最小展示尺寸为 16px，README 使用 112px。单色展示可将琥珀填充和描边统一映射到同一个前景色，并保留中心负形和外轮廓对比。

专题索引见 [README](README.md#目录结构与开发指南)，部署配置见 [deploy/README.md](deploy/README.md)。
