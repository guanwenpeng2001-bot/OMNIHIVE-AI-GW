# 批次四：Windows 原子替换修复

- 工作分支：`guanwenpeng2001-bot/fix-windows-atomic-replace`
- 基线：`d26e1cf9ebc2158f8e809188b3681ce106e57cac`
- 平台：Windows amd64；Go 1.27.0；golangci-lint 2.13.0。
- Task：`task_8c1a9b20c86c`；Dispatch：`ctx_9474c7bac874`。

## 根因与实现

Windows 上替换已存在的文件时，读取句柄可能暂时阻止 `os.Rename`，产生 access denied / sharing violation。原实现直接返回一次替换错误，因此整包并发和生产环境中的短暂文件占用都可能让价格持久化失败。

仅修改两个源码文件：

1. `backend/internal/service/model_capability_catalog.go`：增加可替换的 `renamePricingFile = os.Rename` 函数值，这是唯一依赖结构调整；原 CreateTemp、Chmod、Write、Sync、Close、defer Remove 流程不变。在最终替换处使用 `runtime.GOOS` 限定 Windows，使用 `errors.Is` 识别包装错误内的 Win32 错误码 5（ERROR_ACCESS_DENIED）和 32（ERROR_SHARING_VIOLATION）。最多调用 5 次，4 次退避依次为 25、50、75、100 ms，累计主动等待上限 250 ms；系统调用和调度耗时不计入该等待上限。其他错误立即原样返回，POSIX 仍只调用一次。重试耗尽通过 `%w` 包装最后一次原始错误，同时写入 `5 attempts`，保留原始消息、路径与可检查的错误链。不会先删除目标文件。
2. `backend/internal/service/model_capability_catalog_atomic_test.go`：增加非并行表驱动测试 `TestPricingAtomicReplaceRetry`，临时替换 rename 函数并通过 Cleanup 恢复；用 `testing/synctest` 虚拟时间执行生产退避，无真实 Sleep 等待，也不增加 sleep 或平台测试钩子。

## 新回归用例的断言

- 前两次失败、第三次成功：分别模拟 sharing violation 和 access denied；验证恰好 3 次调用、虚拟耗时 75 ms、旧内容在失败期间完整保留、最终内容成功替换，且一直使用同一个临时文件。
- 始终失败：分别模拟两种错误；验证恰好 5 次调用、虚拟耗时 250 ms、错误包含原始消息和 `5 attempts`，并且 `errors.Is` 可追溯原始 LinkError 和 Errno；旧内容保留，临时文件清理。
- 其他错误：模拟错误码 2，验证一次调用、无等待、原错误对象直接返回。测试在 POSIX 分支也断言模拟错误一律只尝试一次；本次实际执行环境为 Windows，未声称实测 POSIX。

## 验收

所有命令在本子树 `backend/` 下执行。测试命令额外使用 `-json` 记录机器可读事件，其他参数与要求一致；计数包括命名子测试，排除包级 pass 事件。

| 命令 | 退出码 | 结果 |
| --- | --- | --- |
| `go test -json -mod=readonly -tags=unit -run 'TestModelCapability\|TestPricing' -count=3 ./internal/service` | 0 | run 1236 / pass 1236 / skip 0 / fail 0 |
| `go test -json -mod=readonly -tags=unit -p=1 -count=1 -timeout=30m ./internal/service` | 0 | run 15870 / pass 15867 / skip 3 / fail 0；包耗时 194.637 秒 |
| `golangci-lint run --build-tags=unit --new-from-rev=d26e1cf9e ./internal/service/...` | 0 | `0 issues.` |
| `go build -mod=readonly ./...` | 0 | 无输出，成功 |

整包中 `TestPricingAtomicReplaceRetry` 和 `TestModelCapabilityDownloadHashMismatchAndCacheReplay` 都是 pass。三个既有 skip 为 `TestAuthPendingIdentityService_UpsertAdoptionDecision_ClearsLegacyNullSessionReference`、`TestEstimateOpenAIInputTokens_CompareWithOpenAIAPI`、`TestPluginRuntimeIntegration`。

本地完整日志留存在报告同目录：`windows-atomic-targeted.jsonl`、`windows-atomic-full.jsonl`、`windows-atomic-lint.log`、`windows-atomic-build.log`；原始日志不提交，报告和 JSON 回执提交。

## 避免掩盖原始失败

没有修改任何既有测试、断言、skip、缓存重放行为或测试级重试；尤其原 `TestModelCapabilityDownloadHashMismatchAndCacheReplay` 文件完全未改。修复作用于实际生产写文件路径，只重试指定的 Windows 系统错误；其他错误立即暴露，持续占用达到上限仍返回可追溯的原始错误。测试验证失败时保留上一份文件，防止通过删目标文件来绕过原子替换要求。

未改 pricing_service.go、迁移、lint 配置或固定基线；未访问外部数据库或 Redis，未部署、推送、合并或清理。短暂占用恢复已由可控回归测试证明，无法保证持续锁定的目标在上限内恢复，这是有界重试的预期边界。
