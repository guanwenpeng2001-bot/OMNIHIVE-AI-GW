#!/usr/bin/env bash
set -euo pipefail
here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
root=$(cd "$here/../../.." && pwd)
if [[ ${1-} != --inside ]]; then
  duration=${1:-300}
  [[ $duration == --finance || ( $duration =~ ^[1-9][0-9]*$ && $duration -le 7200 ) ]] || { echo 'Load duration must be 1–7200 seconds' >&2; exit 2; }
  # Map root only to bring lo up. Drop it before initdb and candidate startup.
  exec unshare --user --map-root-user --net --pid --fork --mount-proc --kill-child bash -c '
    set -eu
    ip link set lo up
    exec unshare --user --map-user "$1" --map-group "$2" "$3" --inside "$4"
  ' _ "$(id -u)" "$(id -g)" "$here/full.sh" "$duration"
fi
[[ $(ls -1 /sys/class/net | wc -l) -ge 1 ]]
[[ $(ip -o link show | wc -l) -eq 1 && -z $(ip route show default) ]]
umask 077
export GOWORK=off GOPROXY=off GOTOOLCHAIN=local TZ=UTC
export GIN_MODE=release PYTHONDONTWRITEBYTECODE=1
unset PGHOST PGPORT PGUSER PGDATABASE PGSERVICE PGSERVICEFILE PGOPTIONS PGPASSWORD
if [[ -n ${BE24_TOOLS:-} ]]; then
  export PATH="$BE24_TOOLS/postgres16/root/usr/lib/postgresql/16/bin:$BE24_TOOLS/redis7/root/usr/bin:$PATH"
  export LD_LIBRARY_PATH="$BE24_TOOLS/postgres16/root/usr/lib/x86_64-linux-gnu:$BE24_TOOLS/redis7/root/usr/lib/x86_64-linux-gnu:${LD_LIBRARY_PATH:-}"
fi
pg_ctl --version | rg -q ' 16\.'
redis-server --version | rg -q 'v=7\.'
scratch=$(mktemp -d)
export BE24_SCRATCH="$scratch"
scratch_pg=()
redis_pid=
cleanup() {
  [[ -z $redis_pid ]] || { kill "$redis_pid" 2>/dev/null || true; wait "$redis_pid" || true; }
  for cluster in "${scratch_pg[@]}"; do pg_ctl -D "$cluster" -m immediate -w stop >/dev/null || true; done
  rm -rf -- "$scratch"
}
trap cleanup EXIT
trap 'exit 130' INT TERM
for pair in primary:5432 rehearsal:5433; do
  cluster="$scratch/${pair%:*}"
  initdb -D "$cluster" -U postgres --auth=trust --no-instructions >/dev/null
  pg_ctl -D "$cluster" -o "-c listen_addresses=127.0.0.1 -p ${pair#*:} -k $scratch" -l "$cluster/pg.log" -w start >/dev/null
  scratch_pg+=("$cluster")
done
redis-server --bind 127.0.0.1 --port 6379 --save '' --appendonly no --dir "$scratch" >"$scratch/redis.log" 2>&1 &
redis_pid=$!
for db in candidate job_test be09_journal_test previous upgraded; do createdb -h 127.0.0.1 -p 5432 -U postgres "$db"; done
createdb -h 127.0.0.1 -p 5433 -U postgres be09_rehearsal_test
createdb -h 127.0.0.1 -p 5433 -U postgres job_test
export UPSTREAM_JOB_TEST_DSN='postgres://postgres@127.0.0.1:5432/job_test?sslmode=disable'
export BE23_INTEGRATION_TEST_DSN="$UPSTREAM_JOB_TEST_DSN"
export BE09_TEST_JOURNAL_DSN='postgres://postgres@127.0.0.1:5432/be09_journal_test?sslmode=disable'
export BE09_TEST_ISOLATED_DSN='postgres://postgres@127.0.0.1:5433/be09_rehearsal_test?sslmode=disable'
export AFFINITY_REDIS_TEST_ADDR=127.0.0.1:6379 OMNIHIVE_TEST_EXTERNAL_SERVICES=1
cd "$root"
if [[ $2 == --finance ]]; then
  python3 "$here/full.py" --finance
else
  python3 "$here/full.py" --seconds "$2"
fi
