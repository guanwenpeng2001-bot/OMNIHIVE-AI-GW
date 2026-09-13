#!/usr/bin/env bash
# DEPRECATED / 已废弃：仅保留历史证据，禁止执行校验和重写。
# 正式兼容白名单仅由 backend/internal/repository/migrations_runner.go 管理。
# 任意 checksum 不匹配可能代表迁移内容变更，不能批量接受当前文件。
printf '%s\n' 'Deprecated: use reviewed migrations_runner.go compatibility rules; no changes made.' >&2
exit 1
# 修正 dev 库 schema_migrations 的校验和口径：git 规范是 LF（.gitattributes: backend/migrations/*.sql text eol=lf），
# 但 dev 库当年是从 main-work 的 CRLF 工作副本应用迁移的，导致任何按 LF 检出的工作树启动时校验失败。
# 本脚本把 DB 中"与 LF 内容不一致"的行改写为 LF 口径的哈希，并先打印差异（dry-run 默认）。
# 用法：bash fix-migration-checksums.sh [--apply]
set -u
APPLY=0
[ "${1:-}" = "--apply" ] && APPLY=1
ROOT=/c/Users/Administrator/orca/workspaces/sub2api/main-work

PSQL() { docker exec sub2api-postgres psql -U sub2api -d sub2api -t -A -F '|' -c "$1"; }

echo "== 逐文件比较（git blob LF 哈希 vs DB 记录） =="
cd "$ROOT"
mismatch=0
for f in backend/migrations/*.sql; do
  name=$(basename "$f")
  filehash=$(git cat-file blob "HEAD:$f" | node -e "let s='';process.stdin.setEncoding('binary');process.stdin.on('data',d=>s+=d).on('end',()=>{const b=Buffer.from(s,'binary');process.stdout.write(require('crypto').createHash('sha256').update(b.toString('utf8').trim()).digest('hex'))})")
  dbhash=$(PSQL "select coalesce((select checksum from schema_migrations where filename='$name'),'')")
  if [ -z "$dbhash" ]; then
    status="未应用"
  elif [ "$dbhash" = "$filehash" ]; then
    status="一致"
  else
    status="不一致"
    mismatch=$((mismatch+1))
    if [ "$APPLY" = "1" ]; then
      PSQL "update schema_migrations set checksum='$filehash' where filename='$name';" >/dev/null
      echo "  修正 $name: $dbhash -> $filehash"
    else
      echo "  将修正 $name: db=${dbhash:0:16}… file=${filehash:0:16}…"
    fi
  fi
done
echo "不一致数量: $mismatch"

if [ "$APPLY" = "1" ]; then
  echo "== 应用后重启验证：DB 记录全部与 LF 文件一致 =="
  # 把工作副本也统一为 LF（符合 .gitattributes），避免 main-work 再次写入 CRLF 口径
  for f in backend/migrations/*.sql; do
    if grep -q $'\r' "$f" 2>/dev/null; then
      sed -i 's/\r$//' "$f"
      echo "  行尾修正: $f"
    fi
  done
  cd "$ROOT" && git status --porcelain -- backend/migrations | head -5
fi
