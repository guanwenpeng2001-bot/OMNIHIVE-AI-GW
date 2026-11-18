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

## Independence verification

The module-only change passed `go build ./...`, `go vet ./...` and `go test ./... -count=1`. The Go dependency graph resolves through the OmniHive backend module. The plugin descriptor retains its v1 service names and carries the new Go package option.

The deployment gates rendered every Compose variant and verified that an explicitly mapped legacy application volume remains external. The original source Dockerfiles differed in `/app` ownership, which caused the existing deployment parity assertion to fail. The current runtime sections agree, and the parity assertion passes. Caddy, Compose security and gateway settings, Docker resource packaging, installer token handling and installer resource fixtures passed.

A local `omnihive_0.99.0_linux_amd64.tar.gz` fixture contained a real embedded Release build. The native installer verified the checksum, installed the executable and reported its version. A Docker release-recipe build launched the image binary using a mounted volume containing an old staged executable and retained the old file. The Apple Container lifecycle and legacy-volume fixtures passed under Linux with the fixture CLI and macOS command shims. Actual macOS lifecycle and live published-asset cutover remain deployment acceptance checks.

The final frontend build passed. A full Vitest run with six maximum workers passed all 449 files and 5,275 tests, with one test skipped. The two documentation and quickstart contract suites also passed independently.

The final backend passed `go build ./...`, `go vet ./...` and `go test ./...`. The 60 successful packages match the baseline.
