# Refactor Ledger

Status legend: todo / doing / review / merged / blocked

| ID | Work package | Owner model | Branch | Status | Notes |
|---|---|---|---|---|---|
| B0 | Baseline + Go module independence + identity inventory/renames | gpt-6.2-sol | codex/independence | doing | |
| A0 | Backend current state, target architecture, work packages, API contracts | gpt-6.2-astra | codex/architecture | merged | 874358e84 |
| BE-02 | Exact Money, rate and historical fixtures | gpt-6.2-astra | codex/be-02-money | review | 555a92ab7; unit+PG/Redis integration pass; merge after independence |
| BE-01 | Identity, projects and access | gpt-6.2-astra | — | todo | blocked on independence (api_key_auth renames) |
| BE-03 | Attempt controller and stream lifecycle | gpt-6.2-astra | codex/be-03-attempt | doing | based on codex/be-02-money |
| D0 | Reference review, UI audit, design system v1, ui component library | gemini-4-carbon | codex/design-system | doing | |
