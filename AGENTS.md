# AGENTS.md — Development Rules for slack-router

This file defines the conventions, constraints, and security rules for this project.
Follow these rules when writing code, tests, or documentation.

---

## Project Overview

`slack-router` is a Go daemon that receives Slack slash commands via Socket Mode
and routes them to local shell scripts as child processes. It runs as a single
binary with no inbound port. All config lives in `config.yaml`.

---

## Language and Toolchain

- **Go 1.22** — do not use language features beyond Go 1.22
- **`go.mod` module**: `github.com/magifd2/slack-router`
- All files are `package main` — there are no sub-packages
- Use `slices.Contains` (stdlib since Go 1.21); do not write custom slice helpers
- Use `log/slog` for all logging — no `fmt.Println`, no `log.Printf`

---

## File Layout

| File | Responsibility |
|------|----------------|
| `main.go` | Entry point, Socket Mode event loop, signal handling |
| `router.go` | Dispatch, concurrency control, graceful shutdown |
| `config.go` | YAML loading, validation, path resolution |
| `acl.go` | ACL struct, `Check()`, `isEmpty()` |
| `worker.go` | Child process execution, env sanitization, ephemeral notification |
| `*_test.go` | Table-driven unit tests, one file per source file |

Do not add new files unless a clear responsibility boundary justifies it.

---

## Security Rules (non-negotiable)

### Token handling
- **Never** pass `SLACK_APP_TOKEN` or `SLACK_BOT_TOKEN` to worker child processes.
  `sanitizedEnv()` in `worker.go` strips these. If new sensitive env keys are added,
  add them to `sensitiveEnvKeys` in `worker.go`.
- **Never** log `cmd.Text` (slash command arguments). User input may contain passwords
  or tokens. This is enforced in `main.go:handleEvent`.

### SSRF prevention
- All `response_url` values must pass `validateResponseURL()` before any HTTP call.
  Only `https://hooks.slack.com/` is accepted.
- Do not add other allowed hosts without careful review.

### Script execution
- Script paths come from `config.yaml` only — never from user input or Slack events.
  The `//nolint:gosec` comment on `exec.Command` is intentional and must remain.
- `validateScript()` enforces: file exists, is not a directory, is executable (`0100`),
  and is not world-writable (`0002`). Do not relax these checks.
- Script arguments must never come from Slack event data. Pass data via stdin JSON only.

### Worker exit code convention
The router uses `exec.ExitError.ExitCode()` to decide whether to send a user notification
after a worker exits abnormally. Do not change this logic without updating both README and
AGENTS.md.

| ExitCode | Meaning | Router notifies user? |
|---|---|---|
| `0` | success | no |
| `> 0` | script called `exit N` intentionally; script must have sent its own response | no |
| `< 0` | killed by signal (OOM, external SIGKILL, etc.); script could not respond | **yes** |
| startup failure | `cmd.Start` / stdin pipe / encode error | **yes** |

Worker scripts **must** send a user-facing response via `response_url` before exiting with
a non-zero code. If a script exits non-zero without responding, the user sees nothing.

### Path resolution
- Relative script paths are resolved relative to the config file's directory, not CWD.
  This is done in `config.go:validate`. Always use `filepath.Abs` + `filepath.Clean`.

---

## Concurrency and Shutdown Rules

- **`wg.Add` must be called under `r.mu`** to prevent a race with `wg.Wait` during
  shutdown. See `router.go:Dispatch`. Never call `wg.Add` outside this mutex.
- Ephemeral notifications use a **separate `notifyWg`** and are not subject to
  `shuttingDown`. This ensures in-flight notifications complete even during shutdown.
- Use `router.GoNotify(fn)` for all fire-and-forget notification goroutines.
  Never launch bare `go func()` for notifications in `main.go`.
- Workers run in their own process group (`Setpgid: true`) so SIGTERM/SIGKILL hits
  the entire subtree. Do not remove this.

---

## Testing Rules

- **Table-driven tests** — use `[]struct{...}` with subtests (`t.Run`).
- **No mocks** — test real functions directly. Tests in `config_test.go` create
  real temp files; tests in `worker_test.go` test real env manipulation.
- **`t.Setenv`** for env var overrides in tests — it restores values automatically.
- **`t.TempDir()`** for temp files — it cleans up automatically.
- Run tests with: `make test` (`go test -race ./...`). The race detector is mandatory.
- `staticcheck` runs as part of `make lint`. Fix all warnings before committing.

---

## Build and Release

```
make build    # current platform
make test     # go test -race ./...
make lint     # go vet + staticcheck
make release  # cross-compile + zip → dist/
```

### Supported platforms
- `darwin/amd64`, `darwin/arm64`
- `linux/amd64`, `linux/arm64`

Windows is **not supported** — `syscall.Setpgid` and `syscall.Kill` are Unix-only.
Do not add Windows back without resolving process group management.

### Version embedding
Version is embedded at build time via `-ldflags`. The source of truth is `git describe --tags`.
Do not hard-code version strings in Go source.

---

## Change Management

### Keep changes small
- Make one logical change per commit. Avoid bundling unrelated edits.
- If a change touches multiple files, verify each file is necessary for the single goal.

### Ensure rollback points before large changes
- Before starting any non-trivial change (refactor, new feature, dependency upgrade),
  ensure the working tree is clean and the current state is committed or tagged.
- If the work is exploratory and may be discarded, create a throwaway branch rather
  than committing directly to `main`.
- Use `make tag VERSION=vX.Y.Z` to mark a stable state before a risky change.

### Keep AGENTS.md in sync with the code
- When code structure changes (new file added, responsibility moved, new pattern
  established), update the relevant section of this file in the same commit.
- When a new security constraint or concurrency rule is introduced, add it to the
  appropriate section here. Rules that exist only in code comments will be missed.

---

## Adding New Features

### Design for testability first
- Before implementing, identify which functions can be tested in isolation.
  Prefer pure functions and small, focused methods over large procedures.
- If a new function relies on external state (filesystem, env vars, network),
  keep that boundary thin so the core logic can be tested without it.

### Tests are required alongside implementation
- Every new exported or package-level function must have a corresponding test
  in the matching `*_test.go` file.
- Write tests in the same commit as the implementation — do not defer testing
  to a follow-up commit.
- New test cases must follow the table-driven style established in existing test files.

---

## Git and Changelog Rules

- **Conventional commit prefixes**: `feat:`, `fix:`, `refactor:`, `test:`, `docs:`, `chore:`, `security:`
- **CHANGELOG.md** follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format.
  Update `[Unreleased]` section (or add a new version block) with every non-trivial commit.
- Version links at the bottom of CHANGELOG.md must be kept in sync when a new version block is added.
- Tags are created with `make tag VERSION=vX.Y.Z` — do not create tags manually.

---

## What NOT to Do

- Do not read or log `SlashEvent.Text` outside of passing it to worker stdin.
- Do not accept `response_url` as trusted without calling `validateResponseURL`.
- Do not call `wg.Add` outside the shutdown mutex in `router.go`.
- Do not add Windows platform support without solving process group management.
- Do not add sub-packages — keep everything in `package main`.
- Do not use `fmt.Println` or `log` package — use `slog` exclusively.
- Do not write helper utilities that are only used once; prefer inline code.
