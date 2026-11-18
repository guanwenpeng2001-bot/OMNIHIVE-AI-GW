# Refactor Ledger

Status legend: todo / doing / review / merged / blocked

| ID | Work package | Owner model | Branch | Status | Notes |
|---|---|---|---|---|---|
| B0 | Baseline + Go module independence + identity inventory/renames | gpt-6.2-sol | codex/independence | merged | c3560b4d2; follow-ups: plugin v2 signing (BE-20), protocol/persisted keys (BE-30), frontend identity (FE), live cutover (BE-24) |
| A0 | Backend current state, target architecture, work packages, API contracts | gpt-6.2-astra | codex/architecture | merged | 874358e84 |
| BE-02 | Exact Money, rate and historical fixtures | gpt-6.2-astra | codex/be-02-money | merged | 555a92ab7 |
| BE-01 | Identity, projects and access | gpt-6.2-astra | codex/be-01-identity | doing | based on a2831c1a3 |
| BE-03 | Attempt controller and stream lifecycle | gpt-6.2-astra | codex/be-03-attempt | doing | based on codex/be-02-money |
| D0 | Reference review, UI audit, design system v1, ui component library | gemini-4-carbon | codex/design-system | doing | |
