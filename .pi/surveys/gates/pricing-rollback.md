# 价卡差量回滚（替代历史整份恢复操作说明）

本说明基于 audit-ops 的 OPS-03、`prepare-rollback.py` 和
`group4-rollback-merge-only.payload.json`。本工作树原未附 audit-ops；本轮从同仓库
`../main-work/.pi/archive/audit-ops/audit-ops/` 读取权威证据，离线复核材料保存在
`.pi/audit-p2-fixes/`。下列数量是 2026-09-13 审计快照，不是当前生产承诺。

## Group 4：仅撤销 pricing-merge 的 77 张卡

- before 是 84 张，after 是 161 张；审计 current 是 172 张。
- 只删除 `after - before` 的 77 张完整匹配且唯一出现的卡，保留原卡与后续卡。
  审计候选结果应为 95 张（84 + 11），必须与 audit-ops payload 深比较一致。
- 不要发送旧 84 张整份数组，不要删除后续 11 张、路由或 group13 卡。
- 历史 `apply_group4_qianwen.py --rollback` 的 `current == after` 守卫保持原样。
  现在拒绝 172 != 161 是预期保护，禁止为了执行回滚而移除或放宽该守卫。

## Group 13：独立选择目标操作

默认保持 group13 不变。历史 snapshot 是 88 张，审计时是 126 张；38 张差量只是
“这两个快照之间新增”，不能据此认定它们属于同一项应撤销的变更。

需要撤销 group13 的某次操作时，必须先确定那一次操作自己的完整 before/after
快照、来源 commit、同步批次和授权范围。若明确选择撤销整个 88→126 区间，先将
审计 126 张定为该区间 after 并逐卡审查 38 张清单；从**最新** current 中仅移除
这 38 张完整匹配且唯一的卡，保留该区间之后的新卡和非目标编辑。
任何目标卡被编辑、缺失或重复，或操作不是纯追加，立即停止自动生成，逐卡制定
补偿方案；不能用 88 张 snapshot 覆盖最新数组。缺少可归属的 before/after 时
不执行回滚，保留现状并补齐恢复材料。

## 离线生成与实际执行约束

1. 在单写者维护窗口暂停对应组的价格编辑和同步；通过 GET 保存目标组当前数组、
   路由、来源/汇率快照。快照附时间、组 ID、来源 commit 和操作说明，受控保管。
2. 从分组 GET 提取纯 `model_pricing` 数组到 current.json；before/after 也是数组。
   离线生成（只写新本地文件，不登录、不访问网络）：

   ```sh
   python .pi/audit-p2-fixes/prepare-price-rollback.py --before before.json --after after.json --current current.json --output rollback-plan.json
   ```

3. 审查输出 `payload`，确认只移除本次目标卡；保留的卡逐个完整相等。执行前立刻
   GET，要求 `model_pricing == expected_current`，并校验路由与维护窗口快照一致；
   任一漂移则中止并重新生成、审查。API 无原子 CAS，前置 GET 不能替代单写者窗口。
4. 获得实际执行授权后，仅向 `PUT /api/v1/admin/groups/{id}` 发送计划中的
   `payload`（仅 `model_pricing`），通过管理 API 触发正常缓存失效，不直接写 SQL。
   本轮不执行此步骤，不提供绕过守卫的自动 apply 命令。
5. GET 回读必须深等于 payload，核对目标数、保留卡、另一组与路由未变化。
   在获准的隔离可写环境核对代表模型账单后恢复流量；当前共享库禁止推理/写验证。
6. 撤销这次回滚时，以本次 payload 为 after、执行前最新快照为恢复目标，仍要求
   current 深等于本次 after；有新变化则改用另行审查的差量补偿，不能整份覆盖。

这只是价卡恢复方案，不恢复旧账号、分组关联、切流路由或上游配额快照。
历史切流备份缺失不能由本说明补造，也不能宣称完整切流恢复已经演练成功。
