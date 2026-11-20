# QA real smoke

`qa_real_smoke.py` drives the live `omnihive-refactor-qa` deployment through
the developer and admin journeys using only the Python 3 standard library.
It is repeatable, exits non-zero when any executed check fails, and writes
redacted JSON/text evidence under `QA_EVIDENCE_DIR`
(default `/tmp/omnihive-qa-smoke`).

## Run

```bash
python3 scripts/smoke/qa_real_smoke.py
```

Configuration is read from environment variables and, for admin credentials,
from `QA_ENV_FILE` (default
`/home/administrator/omnihive-refactor-qa-deploy/.env`):

| Variable | Default | Purpose |
| --- | --- | --- |
| `QA_BASE` | `http://localhost:18090` | QA deployment base URL |
| `QA_ADMIN_EMAIL` | `ADMIN_EMAIL` in `QA_ENV_FILE` | Admin login email |
| `QA_ADMIN_PASSWORD` | `ADMIN_PASSWORD` in `QA_ENV_FILE` | Admin login password |
| `QA_MAGPIE_KEY` | empty | Optional Magpie gateway key for the create/credential path |
| `QA_ENV_FILE` | `/home/administrator/omnihive-refactor-qa-deploy/.env` | Deploy env file to source credentials |
| `QA_WORKSPACE_ID` / `QA_PROJECT_ID` | auto from `/me/scopes` | Scope pins |
| `QA_GROUP_ID` / `QA_CONNECTION_ID` | auto-detected | Resource pins |

Secrets are never printed or written to evidence. The generated evidence
redacts token/credential/password fields and stores only lengths/state.

## Coverage

- login, compliance (accept skipped when already confirmed), scopes
- v2 connection create/credential/verify/preview/activate
  - with `QA_MAGPIE_KEY`: creates a unique connection and sets the credential
  - without it: reuses the existing verified connection and uses credential
    `keep` so the credential endpoint is still exercised
- group/channel/model-plaza confirmation
- v1 API-key creation and `/v1/models`
- DeepSeek/Kimi/GPT non-streaming `/v1/chat/completions`; upstream failures
  are recorded with their real HTTP status and error reason, never faked
- v1 request logs, v2 billing activity, empty task center
- admin version `check-updates`

## Exit status

- `0` all executed checks passed
- `1` at least one check failed
- `2` configuration/runtime error

Skips (for example compliance already confirmed) are recorded and do not by
themselves fail the run.
