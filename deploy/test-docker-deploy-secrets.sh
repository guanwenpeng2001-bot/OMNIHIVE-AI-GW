#!/usr/bin/env bash
set -eu
script_dir=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
cd "$tmp"
cat > curl <<'MOCK'
#!/bin/sh
for output; do :; done
printf 'JWT_SECRET=\nTOTP_ENCRYPTION_KEY=\nPOSTGRES_PASSWORD=\n' > "$output"
MOCK
chmod +x curl
PATH="$tmp:$PATH" bash "$script_dir/docker-deploy.sh" > output
test "$(stat -c '%a' .env)" = 600
while IFS='=' read -r name value; do
    test "${#value}" = 64
    if grep -Fq "$value" output; then echo "$name leaked to stdout" >&2; exit 1; fi
done < .env
