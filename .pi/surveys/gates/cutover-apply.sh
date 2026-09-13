#!/usr/bin/env bash
# 切流：把两个挂在 openai 平台下的账号原地转换为 qianwen / commandcode 平台，
# 并把 composite-all(group 4) 里属于它们的精确路由重指到新平台。
#
# 用法：
#   bash cutover-apply.sh            # dry-run（只打印将要发生的事）
#   bash cutover-apply.sh --apply    # 真正执行（先建备份表）
#
# 前提：:8080 后端已运行**含 M2/M3/集成轮**的代码；管理员 API 可用。
# 设计原则：只动"账号 5/7 自己的模型集合"，绝不碰其它 openai relay 模型；
#           前缀路由（gpt- 等）保持原样，交由用户决定。
set -euo pipefail
APPLY=0
[ "${1:-}" = "--apply" ] && APPLY=1

BASE=http://127.0.0.1:8080
PSQL() { docker exec sub2api-postgres psql -X -v ON_ERROR_STOP=1 -U sub2api -d sub2api -t -A -F '|' -c "$1"; }

echo "== 0. 登录管理员 =="
TOKEN=$(curl --fail-with-body -sS -X POST "$BASE/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"admin123"}' \
  | node -e "let s='';process.stdin.on('data',d=>s+=d).on('end',()=>{try{process.stdout.write(JSON.parse(s).data.access_token)}catch(e){process.stdout.write('')}})")
if [ -z "$TOKEN" ]; then echo "登录失败，后端未就绪？"; exit 1; fi
echo "token ok"

echo "== 1. 备份 composite 路由与账号/分组平台字段 =="
TS=$(date +%Y%m%d_%H%M%S)_$$
[[ "$TS" =~ ^[A-Za-z0-9_]+$ ]] || { echo "Invalid backup suffix" >&2; exit 1; }
if [ "$APPLY" = "1" ]; then
  # One consistent, locked snapshot; collisions and incomplete backups abort before any PUT.
  PSQL "BEGIN;
    LOCK TABLE composite_model_routes, accounts, groups IN SHARE MODE;
    CREATE TABLE composite_model_routes_backup_$TS AS SELECT * FROM composite_model_routes;
    CREATE TABLE account_platform_backup_$TS AS SELECT id, platform FROM accounts WHERE id IN (5,7);
    CREATE TABLE group_platform_backup_$TS AS SELECT id, platform FROM groups WHERE id IN (11,13);
    DO \$backup\$ BEGIN
      IF (SELECT count(*) FROM composite_model_routes_backup_$TS) <> (SELECT count(*) FROM composite_model_routes)
        OR (SELECT count(*) FROM account_platform_backup_$TS) <> 2
        OR (SELECT count(*) FROM group_platform_backup_$TS) <> 2 THEN
        RAISE EXCEPTION 'Backup row count mismatch; refusing cutover';
      END IF;
    END \$backup\$;
    COMMIT;" >/dev/null
  echo "备份表后缀：$TS"
else
  echo "dry-run：不会建备份表"
fi

echo "== 2. 账号平台转换 =="
for pair in "7:qianwen" "5:commandcode"; do
  AID=${pair%%:*}; PLAT=${pair##*:}
  echo "  账号 $AID  $PLAT"
  if [ "$APPLY" = "1" ]; then
    R=$(curl --fail-with-body -sS -o /tmp/cutover-acct.json -w '%{http_code}' -X PUT "$BASE/api/v1/admin/accounts/$AID" \
      -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
      --data-binary "{\"platform\":\"$PLAT\"}")
    echo "    http=$R $(head -c 160 /tmp/cutover-acct.json)"
  fi
done

echo "== 3. 分组平台转换（11 command-code → commandcode，13 bailian → qianwen） =="
for pair in "13:qianwen" "11:commandcode"; do
  GID=${pair%%:*}; PLAT=${pair##*:}
  echo "  分组 $GID  $PLAT"
  if [ "$APPLY" = "1" ]; then
    R=$(curl --fail-with-body -sS -o /tmp/cutover-group.json -w '%{http_code}' -X PUT "$BASE/api/v1/admin/groups/$GID" \
      -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
      --data-binary "{\"platform\":\"$PLAT\"}")
    echo "    http=$R $(head -c 160 /tmp/cutover-group.json)"
  fi
done

echo "== 4. composite 路由重指（只动 group 4、target=openai、exact、命中账号自有目录） =="
echo -n "  将重指 qianwen: "
PSQL "select count(*) from composite_model_routes where group_id=4 and target_platform='openai' and match_type='exact' and public_model in (select jsonb_object_keys(credentials->'model_mapping') from accounts where id=7);"
echo -n "  将重指 commandcode（排除与 #7 重名）: "
PSQL "select count(*) from composite_model_routes where group_id=4 and target_platform='openai' and match_type='exact' and public_model in (select jsonb_object_keys(credentials->'model_mapping') from accounts where id=5) and public_model not in (select jsonb_object_keys(credentials->'model_mapping') from accounts where id=7);"
echo -n "  重名模型（默认给 commandcode，需用户复核）: "
PSQL "select string_agg(m, ',') from (select jsonb_object_keys(credentials->'model_mapping') m from accounts where id=5 intersect select jsonb_object_keys(credentials->'model_mapping') from accounts where id=7) t;"

if [ "$APPLY" = "1" ]; then
  PSQL "update composite_model_routes set target_platform='qianwen', updated_at=now() where group_id=4 and target_platform='openai' and match_type='exact' and public_model in (select jsonb_object_keys(credentials->'model_mapping') from accounts where id=7);"
  PSQL "update composite_model_routes set target_platform='commandcode', updated_at=now() where group_id=4 and target_platform='openai' and match_type='exact' and public_model in (select jsonb_object_keys(credentials->'model_mapping') from accounts where id=5);"
  echo "  已更新"
fi

echo "== 5. 结果分布（group 4 路由目标平台） =="
PSQL "select target_platform, count(*) from composite_model_routes where group_id=4 group by 1 order by 2 desc;"

echo "== 6. 账号/分组平台现状 =="
PSQL "select id, name, platform, status from accounts where id in (5,7) order by id;"
PSQL "select id, name, platform from groups where id in (11,13) order by id;"

if [ "$APPLY" = "1" ]; then
  echo "== 7. 同步模型 + 连接测试 =="
  for AID in 7 5; do
    echo -n "  账号 $AID refresh: http="
    curl --fail-with-body -sS -o /tmp/cutover-refresh.json -w '%{http_code}\n' -X POST "$BASE/api/v1/admin/accounts/$AID/refresh" -H "Authorization: Bearer $TOKEN"
    echo -n "  账号 $AID test:    http="
    curl --fail-with-body -sS -o /tmp/cutover-test.json -w '%{http_code}\n' -X POST "$BASE/api/v1/admin/accounts/$AID/test" -H "Authorization: Bearer $TOKEN"
    head -c 200 /tmp/cutover-test.json; echo
  done
fi

echo "== 完成（回滚：账号/分组按 *_backup_$TS 还原；路由按 composite_model_routes_backup_$TS 还原） =="
