# OmniHive Refactor Charter (binding for all agents)

Source plan: `docs/refactor/PLAN_v1.0.md` (read the sections relevant to your work package).
Integration branch: `codex/refactor` (based on `dev`). Coordinator: `/root`.

## Why
OmniHive works, but does not *feel* like a product from a serious company. The deliverable is a
fully refactored project that looks and behaves like OpenRouter / Vercel AI Gateway / OpenCode Console:
trustworthy, consistent, precise. "It clicks" is NOT done; every page must pass visual + task review.

## Non-negotiables
- Product identity is **OmniHive** everywhere user-visible (UI, emails, errors, docs, binaries, images,
  services, update feed). No `Sub2API` identity left except in time-boxed migration code and legal notices.
- Keep copyright / license / attribution of inherited code (LICENSE, NOTICE). Never delete provenance.
- No blind global string replace. Auth keys, sessions, plugin manifests/signatures, DB data, config
  paths migrate with explicit versioned compatibility (read old, write new).
- Standard inference APIs (OpenAI / Anthropic / Gemini compatible endpoints) stay wire-compatible.
- Money: exact decimal type end-to-end for new/refactored billing code; documented rounding.
- Backend stays Go + PostgreSQL + Redis, modular monolith. Frontend stays Vue 3 + TypeScript.
- Out of scope: Magpie-style Agent / Connector / multi-agent features.
- No mock/demo data in shipped pages; every page bound to real APIs.

## Design direction (frontend)
- Brand: iterate on honeycomb / charcoal / amber. Amber = the single accent (primary actions, selection,
  key numbers). Large surfaces neutral grays. Honeycomb motif only in logo, empty states, homepage hero.
- Type: Inter (or Geist) for UI text; monospace (JetBrains Mono / Geist Mono) for prices, numbers, model IDs.
- Themes: light + dark share semantic tokens. Marketing/docs default light; console default dark; toggle.
- References (screenshots in `docs/refactor/ui-refs/`):
  - Console shell: OpenCode Console — dark, thin grid lines, uppercase mono nav, restrained color.
  - Model plaza: OpenRouter — left faceted filters, table/list toggle, category tabs, `⌘K` search;
    Fireworks — one-line model summary rows (type · serving · $in · $out · context).
  - Model attributes: Vercel — ✓/✗ columns (ZDR, No training), capability icon row, "+2 more" prices.
  - Enable models/channels: OpenCode toggle cards. Plans: OpenCode Go pricing cards + provider logo row.
  - Benchmarks/compare: heatmap matrix (`benchmark-heatmap.png`).
  - Avoid: GMI-style video backgrounds and glitch effects.
- Every component shows: loading, empty, error, disabled, permission-denied, long text, huge/zero/unknown numbers.
- Copy feedback in place, double-submit prevention, focus return, keyboard + mobile usable.

## Engineering rules
- Work in your own git worktree on a `codex/<workstream>` branch; commit there with clear messages.
  Never commit to `dev`/`main`, never push, never open PRs. `/root` merges into `codex/refactor`.
- Stay inside your file ownership (given in your task). Need to touch another area? Report to `/root`.
- Toolchain: Go at `~/.local/go/bin` (Linux 1.27.4); pnpm at `~/.local/bin/pnpm` (Linux). Always
  `export PATH=$HOME/.local/go/bin:$HOME/.local/bin:$PATH COREPACK_ENABLE_DOWNLOAD_PROMPT=0`.
- Docker: run through `~/.local/bin/withdocker <cmd>`. Name test containers `omnihive-test-*` and clean
  only by exact name. NEVER `docker * prune`.
- Before reporting done: `go build ./... && go vet` on touched packages + relevant `go test`;
  frontend `pnpm run typecheck`/`lint`/`test` as available and `pnpm run build`.
- No new inline comments unless needed for non-obvious logic. No license headers added.
- Report back: branch name, commits, what changed, tests run + results, known gaps.

## Model roster
| Role | Model |
|---|---|
| Architecture / hard backend (billing, routing, gateway split) | `codex/gpt-6.2-astra` (xhigh) |
| Backend implementation | `codex/gpt-6.2-sol` (max / high) |
| Design system / complex pages | `agy-cli/gemini-4-carbon` |
| Frontend model capability | Use the direct agy-cli IDs only: `agy-cli/gemini-4-carbon` (flagship, design system/complex pages/visual review), `agy-cli/gemini-4-neon` >= `agy-cli/gemini-4-argon` (regular implementation), `agy-cli/gemini-4-hydrogen` (small edits/bulk work). Do not use `auto/...` or `group/...` routing groups for frontend work, and never use a GPT model. |
| Frontend implementation | `agy-cli/gemini-4-hydrogen` (NEVER use a GPT model for frontend work) |
| Frontend small edits / i18n | `agy-cli/gemini-3.8-flash` |
| Code review | `workbuddy/deepseek-v4-pro` |
| Tests / regression | `workbuddy/deepseek-v4.1-flash` |
