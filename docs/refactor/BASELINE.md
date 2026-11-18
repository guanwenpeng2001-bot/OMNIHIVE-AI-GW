# Independence baseline

Recorded on 2026-11-19 in the independence worktree at `e385977f2`, using Go 1.27.4, pnpm 12.21.0 and Node 24.19.0.

| Directory | Command | Outcome |
| --- | --- | --- |
| `backend` | `go build ./...` | Passed |
| `backend` | `go vet ./...` | Passed |
| `backend` | `go test ./... -count=1` | Passed, with 60 packages reporting success |
| `frontend` | `pnpm install --frozen-lockfile` | Passed |
| `frontend` | `pnpm run typecheck` | Passed |
| `frontend` | `pnpm run lint:check` | Passed |
| `frontend` | `pnpm run test:run` | First run exited 1 with three Vitest `onTaskUpdate` worker RPC timeouts around `LatePlatformProtocolAlignment.spec.ts`. A subsequent full run passed all 449 files, with 5,275 tests passing and one skipped. |

The frontend suite emits pre-existing mock-network and component-stub diagnostics on passing tests. The two Go checks above and the frontend retry used the charter toolchain. Docker-backed `integration` build tags are outside the listed default Go test invocation.
