#!/usr/bin/env bash
set -Eeuo pipefail

# Repository-wide quality gate. Every stage is independent and idempotent.
#
#   scripts/quality-gate.sh                 # all stages
#   scripts/quality-gate.sh backend         # one stage
#   scripts/quality-gate.sh backend frontend
#   scripts/quality-gate.sh --stage smoke
#
# Test services are disposable. By default the gate starts a private PostgreSQL
# 16 and Redis 7 under .quality-gate/. Set QUALITY_GATE_DATABASE_DSN and
# QUALITY_GATE_REDIS_ADDR only for CI services that are equally disposable.

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

export PATH="${HOME}/.local/go/bin:${HOME}/.local/bin:${PATH}"
export COREPACK_ENABLE_DOWNLOAD_PROMPT=0
export GOFLAGS="${GOFLAGS:--mod=readonly}"

ALL_STAGES=(frontend backend contracts generated-drift independence smoke)
SELECTED=()
KEEP_SERVICES=0

usage() {
  cat <<'EOF'
Usage: scripts/quality-gate.sh [options] [stage ...]

Stages:
  backend          builds, vet, untagged and unit tests, integration tests
  frontend         i18n, typecheck, lint, Vitest and production build
  contracts        JSON protocol fixtures and their consuming tests
  generated-drift  Wire, OpenAPI and openapi.d.ts regeneration must be clean
  independence     new sub2api identifiers require an allowlist entry
  smoke            production binary plus contracts/migrations/*/smoke.sh
  all              every stage (default)

Options:
  -s, --stage NAME  select a comma-separated stage list (repeatable)
  --keep            keep disposable PostgreSQL/Redis/Smoke state after the run
  -l, --list        print stage names and exit
  -h, --help        this text

Environment:
  QUALITY_GATE_DATABASE_DSN  disposable PostgreSQL URL containing /job_test;
                             the server must also provide stats_test,
                             billing_audit_test and backup_data_test
  QUALITY_GATE_STATS_DSN     disposable PostgreSQL URL containing /stats_test
  QUALITY_GATE_REDIS_ADDR    disposable Redis host:port on loopback
  QUALITY_GATE_PG_PORT       isolated PostgreSQL port (default 55480)
  QUALITY_GATE_REDIS_PORT    isolated Redis port (default 56379)
  QUALITY_GATE_LOG_DIR       log directory (default .quality-gate/logs/<run>)
EOF
}

add_stage() {
  local raw="$1" item
  IFS=, read -ra items <<<"$raw"
  for item in "${items[@]}"; do
    item="${item//[[:space:]]/}"
    [[ -z "$item" ]] && continue
    if [[ "$item" == "all" ]]; then
      SELECTED+=("${ALL_STAGES[@]}")
      continue
    fi
    local known=0 candidate
    for candidate in "${ALL_STAGES[@]}"; do
      [[ "$item" == "$candidate" ]] && known=1
    done
    if [[ $known -eq 0 ]]; then
      echo "unknown quality-gate stage: $item" >&2
      exit 2
    fi
    SELECTED+=("$item")
  done
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help) usage; exit 0 ;;
    -l|--list) printf '%s\n' "${ALL_STAGES[@]}"; exit 0 ;;
    --keep) KEEP_SERVICES=1; shift ;;
    -s|--stage)
      [[ $# -ge 2 ]] || { echo "--stage requires a value" >&2; exit 2; }
      add_stage "$2"; shift 2 ;;
    --stage=*) add_stage "${1#*=}"; shift ;;
    -*) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
    *) add_stage "$1"; shift ;;
  esac
done

if [[ ${#SELECTED[@]} -eq 0 ]]; then
  SELECTED=("${ALL_STAGES[@]}")
fi

# Remove duplicates while preserving order.
DEDUPED=()
for stage in "${SELECTED[@]}"; do
  duplicate=0
  for previous in "${DEDUPED[@]}"; do
    [[ "$stage" == "$previous" ]] && duplicate=1
  done
  [[ $duplicate -eq 0 ]] && DEDUPED+=("$stage")
done
SELECTED=("${DEDUPED[@]}")

RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)"
LOG_DIR="${QUALITY_GATE_LOG_DIR:-${ROOT}/.quality-gate/logs/${RUN_ID}}"
mkdir -p "$LOG_DIR" "${ROOT}/.quality-gate/tmp" "${ROOT}/.quality-gate/tools"
ln -sfn "$LOG_DIR" "${ROOT}/.quality-gate/logs/latest"
SUMMARY="${LOG_DIR}/summary.txt"
FAILED=()
declare -A STAGE_SECONDS=()

PG_TEMP=""
PG_DATA=""
PG_SOCKET=""
PG_PORT="${QUALITY_GATE_PG_PORT:-55480}"
PG_BIN=""
PG_DSN=""
STATS_DSN=""
PG_EXTERNAL=0
REDIS_TEMP=""
REDIS_PID=""
REDIS_PORT="${QUALITY_GATE_REDIS_PORT:-56379}"
REDIS_ADDR=""
REDIS_CLI=""
REDIS_EXTERNAL=0
SERVICES_READY=0

log() { printf '[quality-gate] %s\n' "$*"; }

require_command() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "quality-gate requires '$1' in PATH" >&2
    return 1
  }
}

port_free() {
  python3 - "$1" <<'PY'
import socket, sys
s = socket.socket()
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
try:
    s.bind(("127.0.0.1", int(sys.argv[1])))
except OSError:
    sys.exit(1)
finally:
    s.close()
PY
}

picked_port() {
  local preferred="$1" label="$2" port
  for ((port = preferred; port < preferred + 40; port++)); do
    if port_free "$port"; then
      printf '%s' "$port"
      return 0
    fi
  done
  echo "no free loopback port available for ${label} near ${preferred}" >&2
  return 1
}

download_deb() {
  local url="$1" directory="$2" name
  name="$(basename "$url")"
  if [[ ! -f "${directory}/${name}" ]]; then
    curl -fsSL --retry 3 --retry-delay 2 "$url" -o "${directory}/${name}.part"
    mv "${directory}/${name}.part" "${directory}/${name}"
  fi
}

install_postgres16() {
  local prefix="${ROOT}/.quality-gate/tools/postgres16"
  local cache="${ROOT}/.quality-gate/tools/downloads"
  mkdir -p "$cache"
  if [[ ! -x "${prefix}/root/usr/lib/postgresql/16/bin/postgres" ||
        ! -e "${prefix}/root/usr/lib/x86_64-linux-gnu/libicuuc.so.74" ]]; then
    log "installing portable PostgreSQL 16 into ${prefix}"
    local base="http://archive.ubuntu.com/ubuntu/pool"
    local urls=(
      "${base}/main/p/postgresql-16/postgresql-16_16.15-0ubuntu0.24.04.1_amd64.deb"
      "${base}/main/p/postgresql-16/postgresql-client-16_16.15-0ubuntu0.24.04.1_amd64.deb"
      "${base}/main/p/postgresql-16/libpq5_16.15-0ubuntu0.24.04.1_amd64.deb"
      "${base}/main/i/icu/libicu74_74.2-1ubuntu3_amd64.deb"
      "${base}/main/libx/libxml2/libxml2_2.9.14+dfsg-1.3ubuntu3.10_amd64.deb"
    )
    local url
    for url in "${urls[@]}"; do download_deb "$url" "$cache"; done
    mkdir -p "${prefix}/root"
    for url in "${urls[@]}"; do
      dpkg-deb -x "${cache}/$(basename "$url")" "${prefix}/root"
    done
  fi
  PG_BIN="${prefix}/root/usr/lib/postgresql/16/bin"
  export LD_LIBRARY_PATH="${prefix}/root/usr/lib/x86_64-linux-gnu${LD_LIBRARY_PATH:+:${LD_LIBRARY_PATH}}"
}

install_redis7() {
  local prefix="${ROOT}/.quality-gate/tools/redis7"
  local cache="${ROOT}/.quality-gate/tools/downloads"
  mkdir -p "$cache"
  if [[ ! -x "${prefix}/root/usr/bin/redis-server" ]]; then
    log "installing portable Redis 7 into ${prefix}"
    local base="http://archive.ubuntu.com/ubuntu/pool/universe"
    local urls=(
      "${base}/r/redis/redis-server_7.0.15-1ubuntu0.24.04.5_amd64.deb"
      "${base}/r/redis/redis-tools_7.0.15-1ubuntu0.24.04.5_amd64.deb"
      "${base}/libl/liblzf/liblzf1_3.6-4_amd64.deb"
    )
    local url
    for url in "${urls[@]}"; do download_deb "$url" "$cache"; done
    mkdir -p "${prefix}/root"
    for url in "${urls[@]}"; do
      dpkg-deb -x "${cache}/$(basename "$url")" "${prefix}/root"
    done
  fi
  REDIS_CLI="${prefix}/root/usr/bin/redis-cli"
  export LD_LIBRARY_PATH="${prefix}/root/usr/lib/x86_64-linux-gnu${LD_LIBRARY_PATH:+:${LD_LIBRARY_PATH}}"
}

install_jq() {
  local prefix="${ROOT}/.quality-gate/tools/jq"
  local cache="${ROOT}/.quality-gate/tools/downloads"
  mkdir -p "$cache"
  if [[ ! -x "${prefix}/root/usr/bin/jq" ]]; then
    log "installing portable jq into ${prefix}"
    local base="http://archive.ubuntu.com/ubuntu/pool/main"
    local urls=(
      "${base}/j/jq/jq_1.7.1-3build1_amd64.deb"
      "${base}/j/jq/libjq1_1.7.1-3build1_amd64.deb"
      "${base}/libo/libonig/libonig5_6.9.9-1build1_amd64.deb"
    )
    local url
    for url in "${urls[@]}"; do download_deb "$url" "$cache"; done
    mkdir -p "${prefix}/root"
    for url in "${urls[@]}"; do
      dpkg-deb -x "${cache}/$(basename "$url")" "${prefix}/root"
    done
  fi
  export PATH="${prefix}/root/usr/bin:${PATH}"
  export LD_LIBRARY_PATH="${prefix}/root/usr/lib/x86_64-linux-gnu${LD_LIBRARY_PATH:+:${LD_LIBRARY_PATH}}"
}

with_utc_timezone() {
  local dsn="$1"
  if [[ "$dsn" == *"TimeZone="* ]]; then
    printf '%s' "$dsn"
  elif [[ "$dsn" == *"?"* ]]; then
    printf '%s&TimeZone=UTC' "$dsn"
  else
    printf '%s?TimeZone=UTC' "$dsn"
  fi
}

start_postgres() {
  if [[ "${FORCE_ISOLATED:-0}" == "0" && ( -n "${UPSTREAM_JOB_TEST_DSN:-}" || -n "${QUALITY_GATE_DATABASE_DSN:-}" ) ]]; then
    PG_EXTERNAL=1
    PG_DSN="$(with_utc_timezone "${QUALITY_GATE_DATABASE_DSN:-${UPSTREAM_JOB_TEST_DSN}}")"
    STATS_DSN="$(with_utc_timezone "${QUALITY_GATE_STATS_DSN:-${USAGE_STATS_TEST_DSN:-${PG_DSN/\/job_test/\/stats_test}}}")"
    [[ "$PG_DSN" == *"/job_test"* ]] || { echo "QUALITY_GATE_DATABASE_DSN must name job_test" >&2; return 1; }
    [[ "$STATS_DSN" == *"/stats_test"* ]] || { echo "QUALITY_GATE_STATS_DSN must name stats_test" >&2; return 1; }
    log "using explicitly supplied disposable PostgreSQL"
    return 0
  fi

  install_postgres16
  PG_PORT="$(picked_port "$PG_PORT" PostgreSQL)"
  PG_TEMP="$(mktemp -d "${ROOT}/.quality-gate/tmp/pg.XXXXXX")"
  PG_DATA="${PG_TEMP}/data"
  PG_SOCKET="$(mktemp -d /tmp/omnihive-qg-pg.XXXXXX)"
  log "starting isolated PostgreSQL 16 on 127.0.0.1:${PG_PORT}"
  "${PG_BIN}/initdb" -D "$PG_DATA" -U postgres --auth=trust --no-instructions >"${PG_TEMP}/initdb.log" 2>&1
  "${PG_BIN}/pg_ctl" -D "$PG_DATA" -o "-c listen_addresses=127.0.0.1 -c port=${PG_PORT} -c unix_socket_directories=${PG_SOCKET}" -l "${PG_TEMP}/postgres.log" -w start >/dev/null
  "${PG_BIN}/psql" -h 127.0.0.1 -p "$PG_PORT" -U postgres -v ON_ERROR_STOP=1 -c "ALTER USER postgres PASSWORD 'quality_gate'" >/dev/null
  for database in job_test stats_test billing_audit_test backup_data_test be09_rehearsal_test be09_journal_test fix3_batch_test model_access_test ops_billing_backlog_test scheduler_recovery_test scheduler_resources_test prompt_audit_test; do
    "${PG_BIN}/psql" -h 127.0.0.1 -p "$PG_PORT" -U postgres -v ON_ERROR_STOP=1 -c "CREATE DATABASE ${database}" >/dev/null
  done
  local pg_password="quality_gate"
  PG_DSN="postgres://postgres:${pg_password}@127.0.0.1:${PG_PORT}/job_test?sslmode=disable&TimeZone=UTC"
  STATS_DSN="postgres://postgres:${pg_password}@127.0.0.1:${PG_PORT}/stats_test?sslmode=disable&TimeZone=UTC"
}

start_redis() {
  if [[ "${FORCE_ISOLATED:-0}" == "0" && ( -n "${AFFINITY_REDIS_TEST_ADDR:-}" || -n "${QUALITY_GATE_REDIS_ADDR:-}" ) ]]; then
    REDIS_EXTERNAL=1
    REDIS_ADDR="${QUALITY_GATE_REDIS_ADDR:-${AFFINITY_REDIS_TEST_ADDR}}"
    log "using explicitly supplied disposable Redis at ${REDIS_ADDR}"
    return 0
  fi

  install_redis7
  REDIS_PORT="$(picked_port "$REDIS_PORT" Redis)"
  REDIS_TEMP="$(mktemp -d "${ROOT}/.quality-gate/tmp/redis.XXXXXX")"
  log "starting isolated Redis 7 on 127.0.0.1:${REDIS_PORT}"
  LD_LIBRARY_PATH="${ROOT}/.quality-gate/tools/redis7/root/usr/lib/x86_64-linux-gnu${LD_LIBRARY_PATH:+:${LD_LIBRARY_PATH}}" \
    "${ROOT}/.quality-gate/tools/redis7/root/usr/bin/redis-server" \
      --bind 127.0.0.1 --port "$REDIS_PORT" --save "" --appendonly no \
      --dir "$REDIS_TEMP" --logfile "${REDIS_TEMP}/redis.log" --daemonize no &
  REDIS_PID=$!
  for _ in $(seq 1 100); do
    if "$REDIS_CLI" -h 127.0.0.1 -p "$REDIS_PORT" ping >/dev/null 2>&1; then
      break
    fi
    sleep 0.1
  done
  "$REDIS_CLI" -h 127.0.0.1 -p "$REDIS_PORT" ping >/dev/null
  REDIS_ADDR="127.0.0.1:${REDIS_PORT}"
}

ensure_services() {
  [[ $SERVICES_READY -eq 1 ]] && return 0
  start_postgres
  start_redis
  SERVICES_READY=1
}

stop_services() {
  [[ $KEEP_SERVICES -eq 1 ]] && {
    log "keeping disposable services (--keep)"
    return 0
  }
  if [[ -n "$REDIS_PID" ]] && kill -0 "$REDIS_PID" >/dev/null 2>&1; then
    kill "$REDIS_PID" >/dev/null 2>&1 || true
    wait "$REDIS_PID" >/dev/null 2>&1 || true
  fi
  if [[ -n "$PG_DATA" && -x "${PG_BIN}/pg_ctl" ]]; then
    "${PG_BIN}/pg_ctl" -D "$PG_DATA" -m immediate stop >/dev/null 2>&1 || true
  fi
  if [[ -n "$PG_TEMP" && -d "$PG_TEMP" ]]; then rm -rf "$PG_TEMP"; fi
  if [[ -n "$PG_SOCKET" && -d "$PG_SOCKET" ]]; then rm -rf "$PG_SOCKET"; fi
  if [[ -n "$REDIS_TEMP" && -d "$REDIS_TEMP" ]]; then rm -rf "$REDIS_TEMP"; fi
}

reset_test_databases() {
  [[ $PG_EXTERNAL -eq 1 || -z "$PG_BIN" ]] && return 0
  local database
  for database in job_test stats_test model_access_test ops_billing_backlog_test scheduler_recovery_test scheduler_resources_test prompt_audit_test; do
    "${PG_BIN}/psql" -h 127.0.0.1 -p "$PG_PORT" -U postgres -v ON_ERROR_STOP=1 \
      -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='${database}' AND pid<>pg_backend_pid()" \
      -c "DROP DATABASE IF EXISTS ${database} WITH (FORCE)" \
      -c "CREATE DATABASE ${database}" >/dev/null
  done
  if [[ -n "$REDIS_CLI" ]]; then
    "$REDIS_CLI" -h 127.0.0.1 -p "$REDIS_PORT" FLUSHALL >/dev/null 2>&1 || true
  fi
}

finish() {
  local code=$?
  trap - EXIT INT TERM
  stop_services
  exit "$code"
}
trap finish EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

run_stage() {
  local name="$1" start end duration status
  shift
  start="$(date +%s)"
  log "${name}: starting"
  if "$@" >"${LOG_DIR}/${name}.log" 2>&1; then
    status=PASS
  else
    status=FAIL
    FAILED+=("$name")
  fi
  end="$(date +%s)"
  duration=$((end - start))
  STAGE_SECONDS["$name"]=$duration
  printf '%-16s %-4s %6ss  %s\n' "$name" "$status" "$duration" "${LOG_DIR}/${name}.log" | tee -a "$SUMMARY"
  if [[ "$status" == FAIL ]]; then
    echo "----- ${name} failure tail -----" >&2
    if [[ "$name" == smoke && -s "${LOG_DIR}/smoke-failures.log" ]]; then
      cat "${LOG_DIR}/smoke-failures.log" >&2
    fi
    tail -n 40 "${LOG_DIR}/${name}.log" >&2 || true
    echo "-------------------------------" >&2
  fi
}

stage_backend() {
  cd "${ROOT}/backend"
  echo "+ go build ./..."
  go build ./...
  ensure_frontend_dist
  echo "+ go build -tags=embed ./cmd/server"
  go build -tags=embed -o "${ROOT}/.quality-gate/tmp/omnihive-embed" ./cmd/server
  echo "+ go vet ./..."
  go vet ./...
  echo "+ go test -count=1 ./..."
  go test -count=1 ./...

  ensure_services
  export UPSTREAM_JOB_TEST_DSN="$PG_DSN"
  export NEW_PLATFORMS_TEST_POSTGRES_DSN="$PG_DSN"
  export USAGE_STATS_TEST_DSN="$STATS_DSN"
  export CATALOG_SCOPE_TEST_DSN="$PG_DSN"
  export CATALOG_EVENT_TEST_DSN="$PG_DSN"
  export WORKSPACE_TEST_DSN="$PG_DSN"
  export CREDENTIAL_DRAFT_TEST_DSN="$PG_DSN"
  export CREDENTIAL_SECRET_TEST_DSN="$PG_DSN"
  export ACCOUNT_EXTRA_TEST_DSN="$PG_DSN"
  export ACCOUNT_WORKFLOW_TEST_DSN="$PG_DSN"
  export HOSTED_IDENTITY_TEST_DSN="$PG_DSN"
  export USER_BILLING_TIMELINE_TEST_DSN="$PG_DSN"
  export API_KEY_CREDENTIAL_TEST_DSN="$PG_DSN"
  export OPENCODE_QUOTA_TEST_DSN="$PG_DSN"
  export CURSOR_TEST_POSTGRES_DSN="$PG_DSN"
  export CURSOR_MONITOR_TEST_POSTGRES_DSN="$PG_DSN"
  export COMPATIBLE_PROVIDER_TEST_POSTGRES_DSN="$PG_DSN"
  export QUOTES_POSTGRES_DSN="$PG_DSN"
  # These suites compare the raw query string, so keep it to sslmode=disable.
  local base_pg_dsn="${PG_DSN%%&*}"
  export PROMPT_AUDIT_TEST_POSTGRES_DSN="${base_pg_dsn/\/job_test/\/prompt_audit_test}"
  export BE09_TEST_ISOLATED_DSN="${base_pg_dsn/\/job_test/\/be09_rehearsal_test}"
  export BE09_TEST_JOURNAL_DSN="${base_pg_dsn/\/job_test/\/be09_journal_test}"
  export MIGRATION_INTEGRATION_TEST_DSN="$PG_DSN"
  export RETIREMENT_INTEGRATION_TEST_DSN="${base_pg_dsn}"
  export BACKUP_DATA_TEST_DSN="${base_pg_dsn/\/job_test/\/backup_data_test}"
  export UPSTREAM_BILLING_AUDIT_DSN="${PG_DSN/\/job_test/\/billing_audit_test}"
  export BATCH_SUBMIT_TEST_DSN="${base_pg_dsn/\/job_test/\/fix3_batch_test}"
  export MODEL_ACCESS_TEST_DSN="${base_pg_dsn/\/job_test/\/model_access_test}"
  local base_pg_url="${base_pg_dsn%%\?*}"
  export OPS_BILLING_BACKLOG_TEST_DSN="${base_pg_url/\/job_test/\/ops_billing_backlog_test}"
  export SCHEDULER_RECOVERY_TEST_DSN="${base_pg_dsn/\/job_test/\/scheduler_recovery_test}"
  export SCHEDULER_RESOURCES_TEST_DSN="${base_pg_dsn/\/job_test/\/scheduler_resources_test}"
  export AFFINITY_REDIS_TEST_ADDR="$REDIS_ADDR"
  export ACCOUNTING_TEST_REDIS_ADDR="$REDIS_ADDR"
  export CURSOR_TEST_REDIS_ADDR="$REDIS_ADDR"
  export QUOTA_TEST_REDIS_ADDR="$REDIS_ADDR"
  export SECURITY_REDIS_TEST_ADDR="$REDIS_ADDR"
  export PROMPT_AUDIT_TEST_REDIS_ADDR="$REDIS_ADDR"
  export SCHEDULER_RESOURCES_REDIS_ADDR="$REDIS_ADDR"
  export QUOTES_REDIS_ADDR="$REDIS_ADDR"
  export TEST_REDIS_URL="redis://${REDIS_ADDR}/0"

  echo "+ go-test-strict.sh -tags=unit -p 1 -count=1 ./..."
  GO_TEST_STRICT_PROFILE=local "${ROOT}/scripts/go-test-strict.sh" \
    -tags=unit -p 1 -count=1 ./...

  export SUB2API_TEST_EXTERNAL_SERVICES=1
  export OMNIHIVE_TEST_EXTERNAL_SERVICES=1
  echo "+ go-test-strict.sh -p 1 -tags=integration -count=1 ./..."
  if [[ $PG_EXTERNAL -eq 1 ]]; then
    GO_TEST_STRICT_PROFILE=local,integration,local-integration "${ROOT}/scripts/go-test-strict.sh" \
      -p 1 -tags=integration -count=1 ./...
    return
  fi

  local status=0 pkg
  local -a packages=() filtered=()
  mapfile -t packages < <(go list -tags=integration ./...)
  for pkg in "${packages[@]}"; do
    case "$pkg" in
      */internal/control/status|*/internal/repository) ;;
      *) filtered+=("$pkg") ;;
    esac
  done
  GO_TEST_STRICT_PROFILE=local,integration,local-integration "${ROOT}/scripts/go-test-strict.sh" \
    -p 1 -tags=integration -count=1 "${filtered[@]}" || status=1
  for pkg in ./internal/control/status ./internal/repository; do
    echo "+ reset disposable databases before ${pkg}"
    reset_test_databases
    GO_TEST_STRICT_PROFILE=local,integration,local-integration "${ROOT}/scripts/go-test-strict.sh" \
      -p 1 -tags=integration -count=1 "$pkg" || status=1
  done
  return "$status"
}

ensure_frontend_dist() {
  if [[ -f "${ROOT}/backend/internal/web/dist/index.html" ]]; then
    return 0
  fi
  require_command pnpm
  echo "+ frontend assets are required by the embed build"
  (cd "${ROOT}/frontend" && NODE_OPTIONS="${NODE_OPTIONS:---max-old-space-size=4096}" pnpm run build)
}

stage_frontend() {
  node "${ROOT}/scripts/check-frontend-type-drift.mjs" || return $?
  cd "${ROOT}/frontend"
  require_command pnpm
  # The production bundle needs more heap than Node's 2 GB default on 7 GB hosts.
  export NODE_OPTIONS="${NODE_OPTIONS:---max-old-space-size=4096}"
  if [[ ! -x node_modules/.bin/vitest ]]; then
    echo "+ pnpm install --frozen-lockfile"
    pnpm install --frozen-lockfile
  fi
  echo "+ pnpm run check:i18n"
  pnpm run check:i18n
  echo "+ pnpm run typecheck"
  pnpm run typecheck
  echo "+ pnpm run lint:check"
  pnpm run lint:check
  echo "+ pnpm run test:run -- --maxWorkers=6"
  pnpm run test:run -- --maxWorkers=6
  echo "+ pnpm run build"
  pnpm run build
}

stage_contracts() {
  cd "${ROOT}"
  local file count=0
  while IFS= read -r -d '' file; do
    python3 -m json.tool "$file" >/dev/null
    count=$((count + 1))
  done < <(find contracts/protocol contracts/api -type f -name '*.json' -print0 | sort -z)
  echo "validated ${count} protocol and API JSON fixtures"
  cd "${ROOT}/backend"
  # openai_gateway_handler_attempt_test.go is //go:build unit; without the tag
  # the -run below silently matched nothing.
  echo "+ go-test-strict.sh -tags=unit -run '^TestProtocolDecisionFixtures$' ./internal/server/middleware"
  GO_TEST_STRICT_PROFILE=contracts "${ROOT}/scripts/go-test-strict.sh" \
    -tags=unit -count=1 -run '^TestProtocolDecisionFixtures$' ./internal/server/middleware
  echo "+ go-test-strict.sh -tags=unit -run '^(TestAttemptDecisionsAcrossGatewayLoops|TestWebSocketFirstOutputAttemptFixtures)$' ./internal/handler"
  GO_TEST_STRICT_PROFILE=contracts "${ROOT}/scripts/go-test-strict.sh" \
    -tags=unit -count=1 \
    -run '^(TestAttemptDecisionsAcrossGatewayLoops|TestWebSocketFirstOutputAttemptFixtures)$' \
    ./internal/handler
  echo "+ go-test-strict.sh ./internal/gateway/execution/journal ./internal/core/money"
  GO_TEST_STRICT_PROFILE=contracts "${ROOT}/scripts/go-test-strict.sh" \
    -count=1 ./internal/gateway/execution/journal ./internal/core/money
}

stage_generated_drift() {
  cd "${ROOT}/backend"
  local paths=(
    "backend/cmd/server/wire_gen.go"
    "backend/api/openapi/openapi.yaml"
    "frontend/src/api/generated/openapi.d.ts"
  )
  echo "+ go generate ./cmd/server"
  go generate ./cmd/server
  echo "+ go run ./api/openapi/compose"
  go run ./api/openapi/compose
  cd "${ROOT}"
  if [[ ! -d frontend/node_modules ]]; then
    echo "frontend/node_modules is missing; run the frontend stage first" >&2
    return 1
  fi
  echo "+ npm exec openapi-typescript"
  npm exec --yes --package=openapi-typescript@7.10.1 -- \
    openapi-typescript backend/api/openapi/openapi.yaml -o frontend/src/api/generated/openapi.d.ts
  echo "+ git diff --exit-code -- ${paths[*]}"
  git diff --exit-code -- "${paths[@]}"
}

stage_independence() {
  "${ROOT}/scripts/independence-check.sh"
}

wait_for_http_401() {
  local url="$1" pid="$2" code
  for _ in $(seq 1 240); do
    if ! kill -0 "$pid" >/dev/null 2>&1; then
      echo "server exited before becoming ready" >&2
      return 1
    fi
    code="$(curl -sS -o /dev/null -w '%{http_code}' "$url" 2>/dev/null || true)"
    if [[ "$code" != "000" && -n "$code" ]]; then
      return 0
    fi
    sleep 0.5
  done
  echo "server did not become ready at ${url}" >&2
  return 1
}

provision_smoke_key() {
  local base="$1" email="$2" password="$3"
  python3 - "$base" "$email" "$password" <<'PY'
import json
import sys
from urllib.error import HTTPError
from urllib.request import Request, urlopen

base, email, password = sys.argv[1:4]

def call(method, path, expected, body=None, token=None, key=None):
    headers = {"Accept": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    if key:
        headers["Idempotency-Key"] = key
    data = None
    if body is not None:
        headers["Content-Type"] = "application/json"
        data = json.dumps(body).encode()
    request = Request(base + path, method=method, headers=headers, data=data)
    try:
        response = urlopen(request, timeout=30)
    except HTTPError as error:
        response = error
    with response:
        payload = json.load(response)
        if response.status != expected:
            raise SystemExit(f"{method} {path}: expected {expected}, got {response.status}: {payload}")
    return payload.get("data")

login = call("POST", "/api/v1/auth/login", 200, {"email": email, "password": password})
token = login["access_token"]
user_id = login["user"]["id"]
compliance = call("GET", "/api/v1/admin/compliance", 200, token=token)
if compliance["required"]:
    call("POST", "/api/v1/admin/compliance/accept", 200,
         {"phrase": compliance["ack_phrase_en"], "language": "en"}, token=token)
call("POST", f"/api/v1/admin/users/{user_id}/balance", 200,
     {"balance": 10, "operation": "set", "notes": "quality-gate smoke"}, token=token)
call("PUT", "/api/v1/admin/settings", 200,
     {"model_plaza_enabled": True, "model_plaza_require_auth": False}, token=token)
group = call("POST", "/api/v1/admin/groups", 200,
             {"name": "Quality gate smoke", "platform": "openai", "rate_multiplier": 1}, token=token)
key = call("POST", "/api/v1/keys", 200,
           {"name": "Quality gate smoke", "group_id": group["id"]}, token=token)
print(key["secret"])
PY
}

stage_smoke() {
  cd "${ROOT}"
  require_command openssl
  install_jq
  ensure_frontend_dist
  if [[ $PG_EXTERNAL -eq 1 || $REDIS_EXTERNAL -eq 1 ]]; then
    FORCE_ISOLATED=1
    PG_EXTERNAL=0
    REDIS_EXTERNAL=0
    SERVICES_READY=0
    ensure_services
  else
    ensure_services
  fi
  local build_dir="${ROOT}/.quality-gate/tmp/smoke.$$"
  local data_dir="${build_dir}/data"
  local fixture_dir="${build_dir}/tls"
  mkdir -p "$data_dir" "$fixture_dir"
  local server_port
  server_port="$(picked_port 18091 smoke)"

  echo "+ go build -tags=embed ./cmd/server"
  (cd "${ROOT}/backend" && go build -tags=embed -o "${build_dir}/omnihive" ./cmd/server)

  if [[ $PG_EXTERNAL -eq 0 ]]; then
    "${PG_BIN}/psql" -h 127.0.0.1 -p "$PG_PORT" -U postgres -v ON_ERROR_STOP=1 \
      -c "DROP DATABASE IF EXISTS omnihive_smoke" \
      -c "CREATE DATABASE omnihive_smoke" >/dev/null
  else
    echo "external PostgreSQL mode requires an empty omnihive_smoke database" >&2
    return 1
  fi

  openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj '/CN=localhost' \
    -addext 'subjectAltName=IP:127.0.0.1,DNS:localhost' \
    -keyout "${fixture_dir}/fixture.key" -out "${fixture_dir}/fixture.pem" >/dev/null 2>&1

  local admin_email="quality-gate@example.invalid"
  local admin_password="QualityGate-Smoke-Only-9x7!"
  local jwt_secret
  jwt_secret="$(python3 -c 'import secrets; print(secrets.token_hex(32))')"
  local totp_key
  totp_key="$(python3 -c 'import secrets; print(secrets.token_hex(32))')"

  export AUTO_SETUP=true
  export OMNIHIVE_DATA_DIR="$data_dir"
  export CONFIG_FILE="${data_dir}/config.yaml"
  export DATABASE_HOST=127.0.0.1
  export DATABASE_PORT="$PG_PORT"
  export DATABASE_USER=postgres
  export DATABASE_PASSWORD=quality_gate
  export DATABASE_DBNAME=omnihive_smoke
  export DATABASE_SSLMODE=disable
  export REDIS_HOST=127.0.0.1
  export REDIS_PORT="$REDIS_PORT"
  export REDIS_DB=0
  export ADMIN_EMAIL="$admin_email"
  export ADMIN_PASSWORD="$admin_password"
  export SERVER_HOST=127.0.0.1
  export SERVER_PORT="$server_port"
  export SERVER_MODE=release
  export JWT_SECRET="$jwt_secret"
  export TZ=UTC
  export SSL_CERT_FILE="${fixture_dir}/fixture.pem"

  echo "+ bootstrap server (auto setup)"
  "${build_dir}/omnihive" >"${build_dir}/bootstrap.log" 2>&1 &
  local bootstrap_pid=$!
  for _ in $(seq 1 240); do
    if [[ -f "${data_dir}/.installed" && -f "$CONFIG_FILE" ]]; then
      break
    fi
    if ! kill -0 "$bootstrap_pid" >/dev/null 2>&1; then
      echo "bootstrap server exited unexpectedly" >&2
      tail -n 40 "${build_dir}/bootstrap.log" >&2 || true
      return 1
    fi
    sleep 0.5
  done
  [[ -f "$CONFIG_FILE" ]] || { echo "bootstrap did not write config" >&2; return 1; }
  kill "$bootstrap_pid" >/dev/null 2>&1 || true
  wait "$bootstrap_pid" >/dev/null 2>&1 || true

  cat >>"$CONFIG_FILE" <<EOF
security:
  url_allowlist:
    enabled: true
    upstream_hosts:
      - 127.0.0.1
    allow_private_hosts: true
    allow_insecure_http: true
totp:
  encryption_key: "${totp_key}"
EOF

  unset AUTO_SETUP
  echo "+ start production embed binary on 127.0.0.1:${server_port}"
  "${build_dir}/omnihive" >"${build_dir}/server.log" 2>&1 &
  local server_pid=$!
  wait_for_http_401 "http://127.0.0.1:${server_port}/api/v2/me/scopes" "$server_pid" || {
    tail -n 60 "${build_dir}/server.log" >&2 || true
    kill "$server_pid" >/dev/null 2>&1 || true
    return 1
  }

  export SMOKE_BASE_URL="http://127.0.0.1:${server_port}"
  export SMOKE_ADMIN_EMAIL="$admin_email"
  export SMOKE_ADMIN_PASSWORD="$admin_password"
  export SMOKE_TLS_CERT="${fixture_dir}/fixture.pem"
  export SMOKE_TLS_KEY="${fixture_dir}/fixture.key"
  local smoke_api_key
  smoke_api_key="$(provision_smoke_key "$SMOKE_BASE_URL" "$admin_email" "$admin_password")"
  export SMOKE_API_KEY="$smoke_api_key"
  export SMOKE_MODEL="${SMOKE_MODEL:-gpt-4o-mini}"
  export SMOKE_EXPECT_STATUS="${SMOKE_EXPECT_STATUS:-503}"
  export UPSTREAM_JOB_TEST_DSN="$PG_DSN"
  export AFFINITY_REDIS_TEST_ADDR="$REDIS_ADDR"
  export QUOTES_POSTGRES_DSN="$PG_DSN"
  export QUOTES_REDIS_ADDR="$REDIS_ADDR"
  export MIGRATION_INTEGRATION_TEST_DSN="${PG_DSN/&TimeZone=UTC/}"
  export RETIREMENT_INTEGRATION_TEST_DSN="${PG_DSN/&TimeZone=UTC/}"
  if [[ $PG_EXTERNAL -eq 0 ]]; then
    reset_test_databases
  fi
  local catalog_ready=0
  if curl -fsS "${SMOKE_BASE_URL}/api/v2/public/models?limit=1" |
    python3 -c 'import json,sys; sys.exit(0 if json.load(sys.stdin)["data"]["items"] else 1)' 2>/dev/null; then
    catalog_ready=1
  fi

  local failed=0 script
  while IFS= read -r -d '' script; do
    if [[ "$script" == *"/catalog/smoke.sh" && $catalog_ready -eq 0 ]]; then
      echo "SKIP ${script}: requires a catalog fixture (visible account/channel/group model)"
      continue
    fi
    if [[ "$script" == *"/restore/smoke.sh" && -z "${SMOKE_BACKUP_ID:-}" ]]; then
      echo "SKIP ${script}: requires a completed fixture backup ID"
      continue
    fi
    if [[ ! -x "$script" ]]; then chmod +x "$script"; fi
    echo "+ bash ${script}"
    local script_log
    script_log="${LOG_DIR}/smoke-$(basename "$(dirname "$script")").log"
    if ! bash "$script" 2>&1 | tee "$script_log"; then
      echo "smoke failed: ${script}" >&2
      {
        echo "smoke failed: ${script}"
        tail -n 25 "$script_log" | sed 's/^/  | /'
      } >>"${LOG_DIR}/smoke-failures.log"
      failed=1
    fi
  done < <(find contracts/migrations -mindepth 2 -maxdepth 2 -name smoke.sh -print0 | sort -z)

  kill "$server_pid" >/dev/null 2>&1 || true
  wait "$server_pid" >/dev/null 2>&1 || true
  if [[ $KEEP_SERVICES -eq 1 ]]; then
    log "keeping smoke state at ${build_dir}"
  else
    rm -rf "$build_dir"
  fi
  return "$failed"
}

echo "quality gate ${RUN_ID}" | tee "$SUMMARY"
echo "commit: $(git rev-parse --short HEAD)  branch: $(git rev-parse --abbrev-ref HEAD)" | tee -a "$SUMMARY"
echo "logs: ${LOG_DIR}" | tee -a "$SUMMARY"

for stage in "${SELECTED[@]}"; do
  run_stage "$stage" "stage_${stage//-/_}"
done

echo "----- summary -----" | tee -a "$SUMMARY"
cat "$SUMMARY"
if [[ ${#FAILED[@]} -gt 0 ]]; then
  echo "failed stages: ${FAILED[*]}" >&2
  exit 1
fi
echo "all selected stages passed"
