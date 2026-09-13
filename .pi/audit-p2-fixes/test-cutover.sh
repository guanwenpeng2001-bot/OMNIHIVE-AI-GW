#!/usr/bin/env bash
set -euo pipefail
curl() {
  if [[ "$*" == *'/auth/login'* ]]; then
    printf '%s' '{"data":{"access_token":"offline-test"}}'
  else
    printf '%s\n' "UNEXPECTED NETWORK CALL $*" >> .pi/audit-p2-fixes/mock-network.log
    return 91
  fi
}
docker() {
  printf '%s\n' "$*" > .pi/audit-p2-fixes/backup-sql.txt
  return 23
}
export -f curl docker
if bash .pi/surveys/gates/cutover-apply.sh --apply > .pi/audit-p2-fixes/cutover-failure.log 2>&1; then
  echo 'FAIL: ignored backup failure'; exit 1
else
  code=$?
  [[ "$code" == 23 ]] || { echo "Unexpected exit $code"; exit 1; }
fi
[[ ! -e .pi/audit-p2-fixes/mock-network.log ]] || { echo 'FAIL: called mutation after backup failure'; exit 1; }
echo 'PASS: backup failure exits before account/group/route mutations'
