# 审计 P2 修复交付报告

任务规格：`.pi/audit-p2-fixes/task-spec.md`。工作树 `audit-p2-fixes`；2026-09-13。

9 条修复均已实现。未 push，未自起实例，未进行管理员登录、推理、切流、价卡回滚或共享数据库写入。

## 逐条结果

| # | 修复与文件 | 证据和验证 | 未做及原因 |
|---|---|---|---|
| 1 | `frontend/src/i18n/locales/{zh,en}/admin/accounts.ts` 的 commandcodeNote 改为实验性 alpha 用量/额度查询及失败回落本地统计，保留无 OAuth、公共模型目录不能验证 Key、套餐权限说明 | NewPlatformNotice 与 UseKeyModal 均复用该翻译键；审计前端 F3，后端 commandcode_usage.go 的 alpha 路径；UseKeyModal 29 测试通过 | 不触发真实 alpha 查询，查询会持久化账号观测数据 |
| 2 | 同文件 localUsage / pricingUnsupported 区分通用 provider 映射与千问北京区人民币原生流程 | 审计前端 F4；原生面板 qianwenPricing 文案与通用说明一致；i18n 完整性 3 测试通过 | 不调用原生价格预览/应用，两者均有写入副作用 |
| 3 | `AccountUsageCell.vue` 将 Zhipu/MiniMax 非 coding 无上游模式及 OpenCode Zen 路由至已有今日统计分支；显式显示仅本地统计、req、tokens、成本；Ollama eligible 保留专用路径 | 使用 cnQuotaCellVisible / cnBalanceCellVisible 权威 helper，未复制模式枚举；AccountsView 为所有账号获取今日数据；新增三平台无数据、加载、正常、零值、切换 coding/go 回归，AccountUsageCell 共54测试通过 | 真机没有这些模式的有效样本，使用组件测试；未创建样本或探测上游 |
| 4 | `CreateAccountModal.vue` / `EditAccountModal.vue` 保留 Anthropic 专属提示，其他 fallback 使用 platformBaseUrlHint：默认地址由所选平台和账号模式决定；zh/en 同步 | 审计前端 F6；创建45、编辑85测试通过；typecheck/build通过 | 未改平台真实端点或认证协议 |
| 5 | `UserPlatformQuotaModal.vue` 保存和重置错误统一读取规范化 message，其次 reason，再本地化兜底 | 16测试通过；新增 message优先、reason-only、空对象兜底在保存和重置两条路径的校验，验证失败不关闭、按钮解除忙状态 | 不真实提交额度保存/重置，禁止写共享库 |
| 6 | `.pi/surveys/gates/cutover-apply.sh` 使用 set -euo pipefail、psql -X -v ON_ERROR_STOP=1、时间下划线+PID后缀并限定字符集；备份在锁定三表的同一事务内创建并验证路由行数相等、账号/组各2行；不再 drop 同名备份；curl HTTP错误退出 | bash -n通过；离线 curl/docker mock 验证 psql备份失败退出23，任何账号/组/路由修改均未触发；生成SQL见 backup-sql.txt | 不在共享库创建备份/锁表/执行切流；未声明 PostgreSQL 真写备份演练通过 |
| 7 | 新增 `.pi/surveys/gates/pricing-rollback.md` 替代旧整份恢复说明，包含 group4 77张差量及 group13 独立目标区间方案；新增离线 prepare-price-rollback.py | audit-ops OPS-03，离线当前172→95完全等于原审计payload，原84和后来11张保留；4项离线测试含后续卡、非目标编辑、group13模拟38张区间、缺失/重复/目标卡改动/非追加拒绝；历史 apply_group4_qianwen.py current==after 守卫未改 | 未执行PUT或账单验证；group13默认不动，必须选择可归属的操作before/after后再生成真实方案；历史备份缺失仍不代表已完成整套切流恢复 |
| 8 | 删除 `backend/internal/service/account_service.go` 末尾 AccountService.TestCredentials 空实现 | 仓库全量rg确认只有方法定义、无业务或测试引用；原实现对列出平台无条件返回nil、对其他平台报错；实际凭证测试业务路径未改变 | 不新增无必要的测试，不改实际平台权威helper |
| 9 | 历史 `.pi/surveys/gates/fix-migration-checksums.sh` 从当前工作树移至 `.pi/archive/deprecated-tools/fix-migration-checksums.sh`，顶部明确废弃并直接exit1，历史代码仅作证据 | --apply离线调用仍退出1且无任何工具调用；正式兼容仍仅在 backend/internal/repository/migrations_runner.go，未扩大任何白名单 | 未改迁移SQL或数据库checksum |

## 验证记录

- `pnpm typecheck`：exit0。
- `pnpm build`：exit0；包含3项i18n完整性测试。只出现 Browserslist 数据过旧、Node弃用及既有大chunk警告。
- 两组关键组件：70/70；创建、编辑、UseKey：159/159，共229项组件测试。
- `python .pi/audit-p2-fixes/test-rollback.py`：4/4；其中真实审计group4候选深比较一致。
- Bash语法、备份失败停止测试通过；归档危险工具--apply拒绝执行（exit1）。
- `go build ./...`：exit0；`go vet ./...`：exit0；`go test -tags=unit ./...`：全量通过，exit0（包含 service、repository、迁移兼容单测）。原始日志为 go-build.log、go-vet.log、go-unit.log 和 go-results.txt。
- `git diff --check`：全部提交差异通过；最终源码仅清理了空行和撤回非目标Gemini标签修改，不改变已验证的功能逻辑。
- 真实已运行 `http://127.0.0.1:8080/health`：HTTP200，`{"status":"ok"}`，见live-health.txt。未修改、重启或替换该服务；此健康结果不等于已部署本树代码。
- 未执行管理员登录：`auth_handler.go:281`调用 RecordSuccessfulLogin，最终 `auth_service.go:1024`更新LastLoginAt/LastActiveAt，会违反不写共享库约束；不采用认证绕过。

## 材料来源与交接注意

初始git status只有未跟踪`.pi/`，受版本控制源码无预存修改。本树附带audit-backend/audit-billing/audit-cross/audit-frontend，但缺audit-ops和前端最终report；只读查找后，从同仓库`../main-work/.pi/archive/audit-ops/audit-ops/`和`../main-work/.pi/archive/audit-frontend/audit-frontend/report.md`读取了权威材料。定价历史快照和带守卫apply脚本来自`../main-work/.pi/archive/pricing-merge/pricing-merge/`；没有修改该兄弟工作树。test-rollback.py是本次过程测试，真实审计用例依赖这些兄弟归档；其余用例为自含离线数据。

`.pi`历史文件原本均未跟踪，所以本轮只精确加入必要脚本、文档与报告，不提交整棵归档或测试凭据。旧checksum脚本在本树已不存在；若以cherry-pick交接到另一棵仍有该**未跟踪**旧脚本的工作树，需同步删除旧`.pi/surveys/gates/fix-migration-checksums.sh`副本，Git不能表达原先未跟踪文件的删除。新的归档副本已跟踪并不可执行危险逻辑。

## 提交（均未push）

- `2a1d547a9` fix(frontend): clarify platform usage and surface quota errors
- `a104b3bc0` refactor(accounts): remove unused credential test stub
- `a7118160a` fix(ops): guard cutover backups and document differential rollback
- `3e3c5e575` fix(ops): terminate backup validation block explicitly
- `130e7eb65` chore: keep audit fixes scoped and whitespace clean

## 最终状态

9项代码/运维材料修复完成，要求的构建、静态检查和全量unit均通过。共享库写验证、真实管理员交互、切流及价卡恢复演练未执行，原因与执行边界如上；没有将健康探活冒充本树部署验收。运行日志与离线证据保留在本目录，报告和必要修复文件已提交，其他既有.pi材料保持未跟踪。
