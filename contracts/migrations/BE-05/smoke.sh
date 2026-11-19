#!/usr/bin/env bash
set -euo pipefail
: "${SMOKE_BASE_URL:?}" "${SMOKE_ADMIN_EMAIL:?}" "${SMOKE_ADMIN_PASSWORD:?}"
base=${SMOKE_BASE_URL%/}
payload=$(jq -n --arg email "$SMOKE_ADMIN_EMAIL" --arg password "$SMOKE_ADMIN_PASSWORD" '{email:$email,password:$password}')
session=$(curl --fail-with-body -sS -H 'Content-Type: application/json' -d "$payload" "$base/api/v1/auth/login" | jq -er '.data.access_token')
auth=(-H "Authorization: Bearer $session")
scopes=$(curl --fail-with-body -sS "${auth[@]}" "$base/api/v2/me/scopes")
workspace=$(jq -er '.data.items[0].workspace_id' <<< "$scopes")
project=$(jq -er '.data.items[0].projects[0].id' <<< "$scopes")
public=$(curl --fail-with-body -sS "$base/api/v2/public/models?limit=1")
jq -e '.data.items | type == "array" and all(.access_state == "requires_scope" and (has("effective_scope")|not))' <<< "$public" > /dev/null
url="$base/api/v2/workspaces/$workspace/projects/$project/models"
scoped=$(curl --fail-with-body -sS "${auth[@]}" "$url?limit=1")
jq -e --arg w "$workspace" --arg p "$project" '.data.items | type == "array" and all(.effective_scope.workspace_id == $w and .effective_scope.project_id == $p)' <<< "$scoped" > /dev/null

check_model() {
  local endpoint=$1 list=$2
  shift 2
  local id version response compare cursor next
  id=$(jq -er '.data.items[0].id // empty' <<< "$list")
  version=$(jq -er '.data.catalog_version' <<< "$list")
  cursor=$(jq -r '.data.next_cursor // empty' <<< "$list")
  if [[ -n $cursor ]]; then
    next=$(curl --fail-with-body -sS "$@" --get --data-urlencode "cursor=$cursor" --data-urlencode 'limit=1' "$endpoint")
    jq -e --arg id "$id" --arg version "$version" '.data.catalog_version == $version and .data.items[0].id != $id' <<< "$next" > /dev/null
  fi
  response=$(curl --fail-with-body -sS "$@" "$endpoint/$id?catalog_version=$version")
  jq -e --arg id "$id" '.data.model.id == $id and (.data.metrics | has("state"))' <<< "$response" > /dev/null
  compare=$(curl --fail-with-body -sS "$@" -H 'Content-Type: application/json' \
    -d "$(jq -n --arg id "$id" '{model_ids:[$id]}')" "$endpoint/compare")
  jq -e --arg id "$id" '.data.rows[0].model_id == $id' <<< "$compare" > /dev/null
}
check_model "$base/api/v2/public/models" "$public"
check_model "$url" "$scoped" "${auth[@]}"
printf 'BE-05 catalog smoke passed\n'
