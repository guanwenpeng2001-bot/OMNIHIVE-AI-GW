#!/usr/bin/env bash
set -euo pipefail

# Fails on sub2api identifiers that are neither legal attribution nor an
# explicitly persisted compatibility key. See contracts/identity/IDENTITY_ALLOWLIST.md.

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
ALLOWLIST="${ROOT}/contracts/identity/IDENTITY_ALLOWLIST.md"

[[ -f "$ALLOWLIST" ]] || { echo "missing allowlist: ${ALLOWLIST}" >&2; exit 1; }

require_command() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "independence check requires '$1' in PATH" >&2
    exit 1
  }
}
require_command python3

paths=(
  backend
  frontend
  deploy
  contracts
  .github
  Makefile
  scripts
)

# Immutable history and generated/vendored assets are deliberately outside the
# identifier gate. Runtime source, deploy scripts and CI are included.
exclude=(
  ':(exclude)backend/migrations/**'
  ':(exclude)backend/pkg/pluginapi/v1/*.pb.go'
  ':(exclude)frontend/pnpm-lock.yaml'
  ':(exclude)**/node_modules/**'
  ':(exclude)docs/**'
  ':(exclude)**/*_test.go'
  ':(exclude)**/__tests__/**'
  ':(exclude)**/*.spec.ts'
  ':(exclude)**/*.snap'
  ':(exclude)contracts/**'
  ':(exclude)contracts/identity/IDENTITY_ALLOWLIST.md'
  ':(exclude)scripts/independence-check.sh'
  ':(exclude)scripts/quality-gate.sh'
)

read -r -d '' checker <<'PY' || true
import re
import sys

allowlist_path = sys.argv[1]
entries = []
with open(allowlist_path, encoding="utf-8") as handle:
    for number, raw in enumerate(handle, 1):
        line = raw.rstrip("\n")
        if not line.strip() or line.lstrip().startswith("#") or line.strip().startswith("```"):
            continue
        if "\t" not in line:
            continue
        parts = line.split("\t")
        if len(parts) != 3:
            raise SystemExit(f"{allowlist_path}:{number}: expected path, token and reason")
        path_re, token_re, reason = parts
        # The historical directory-wide deploy entry is too broad. Replace it
        # with the precise legacy-compatibility file list below so any new
        # deploy line must be explicitly reviewed instead of slipping through.
        if path_re == "^deploy/":
            continue
        entries.append((re.compile(path_re), re.compile(token_re, re.IGNORECASE), reason))

# Precise deploy compatibility allowlist: only legacy upgrade/migration paths
# that must keep the old identity, never the whole deploy directory.
deploy_entries = (
    (r"^deploy/apple-container\.sh$", r"sub2api"),
    (r"^deploy/install\.sh$", r"sub2api"),
    (r"^deploy/docker-entrypoint\.sh$", r"sub2api"),
    (r"^deploy/docker-compose(?:\.local|\.standalone|\.dev)?\.yml$", r"SUB2API_[A-Z0-9_]+"),
    (r"^deploy/tests/apple-container-test\.sh$", r"sub2api"),
    (r"^deploy/tests/fixtures/bin/container$", r"sub2api"),
    (r"^deploy/tests/install-pricing-resources-test\.sh$", r"sub2api"),
)
for path_re, token_re in deploy_entries:
    entries.append((re.compile(path_re), re.compile(token_re, re.IGNORECASE), "deploy legacy compatibility"))

violations = []
checked = 0
for raw in sys.stdin:
    match = raw.rstrip("\n").split(":", 2)
    if len(match) != 3:
        continue
    file, number, text = match
    checked += 1
    if any(path_re.search(file) and token_re.search(text) for path_re, token_re, _ in entries):
        continue
    violations.append((file, number, text))

if violations:
    for file, number, text in violations:
        print(f"{file}:{number}:{text}", file=sys.stderr)
    print(f"independence: {len(violations)} non-allowlisted sub2api identifier line(s) ({checked} scanned)", file=sys.stderr)
    print("new sub2api identifier requires contracts/identity/IDENTITY_ALLOWLIST.md entry", file=sys.stderr)
    raise SystemExit(1)

print(f"independence: {checked} existing sub2api identifier line(s) covered by the allowlist")
PY

matches=""
if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  set +e
  matches="$(git grep -inI 'sub2api' -- "${paths[@]}" "${exclude[@]}")"
  git_status=$?
  set -e
  case "$git_status" in
    0) ;; # matches found
    1) matches="" ;; # no matches: pass
    *)
      echo "independence: git grep failed with exit ${git_status}" >&2
      exit "$git_status"
      ;;
  esac
else
  # Exported snapshots (for example /tmp/omnihive-accept/baseline) have no
  # .git directory. Fall back to ripgrep over the same protected file set and
  # honour the same exclusions; a real scanner error is fatal instead of being
  # reported as "no identifiers found".
  require_command rg
  set +e
  matches="$(rg -inI --no-heading --with-filename 'sub2api' \
    -g '!backend/migrations/**' \
    -g '!backend/pkg/pluginapi/v1/*.pb.go' \
    -g '!frontend/pnpm-lock.yaml' \
    -g '!**/node_modules/**' \
    -g '!docs/**' \
    -g '!**/*_test.go' \
    -g '!**/__tests__/**' \
    -g '!**/*.spec.ts' \
    -g '!**/*.snap' \
    -g '!contracts/**' \
    -g '!scripts/independence-check.sh' \
    -g '!scripts/quality-gate.sh' \
    -- "${paths[@]}")"
  rg_status=$?
  set -e
  case "$rg_status" in
    0) ;; # matches found
    1) matches="" ;; # no matches: pass
    *)
      echo "independence: ripgrep failed with exit ${rg_status}" >&2
      exit "$rg_status"
      ;;
  esac
fi

if [[ -z "$matches" ]]; then
  echo "independence: no sub2api identifiers found in the protected scan"
  exit 0
fi
printf '%s\n' "$matches" | python3 -c "$checker" "$ALLOWLIST"
