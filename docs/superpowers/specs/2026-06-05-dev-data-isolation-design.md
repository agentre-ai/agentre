# Dev-isolated data root

**Date:** 2026-06-05
**Status:** Approved (design)
**Scope:** `agentre` desktop app

## Problem

`make dev` (which runs `wails dev`) and the installed `/Applications/Agentre.app` both
resolve `paths.AppDataDir()` to the **same** root — on macOS `~/Library/Application
Support/agentre/`. That single root holds the SQLite DB (`agentre.db`), `logs/`, the cago
config, and agent working copies (`agentruntime/cwd.go`). So debugging via `make dev`
mutates the user's real sessions/DB/logs, and the two builds cannot run side by side
safely. We want the dev instance to write to a **separate** data root, automatically.

## Goal

- A `wails dev` instance writes all desktop state to a data root distinct from the
  installed app's, with no manual env management.
- The installed app's behavior and path are **unchanged**.
- The explicit `AGENTRE_DATA_DIR` override keeps winning over everything (tests, custom
  setups).
- Both builds can run concurrently (no SQLite file contention, no single-instance clash).

## Approach: dev-aware `paths.AppDataDir()`

Change the one chokepoint every piece of desktop state already routes through.
New precedence in `paths.AppDataDir()` (highest first):

1. `AGENTRE_DATA_DIR` set (non-empty, trimmed) → return it verbatim. *(unchanged)*
2. dev mode (`devserver` env present) → `<UserConfigDir>/agentre-dev`. *(new)*
3. otherwise → `<UserConfigDir>/agentre`. *(unchanged)*

macOS result: dev → `~/Library/Application Support/agentre-dev/`, installed →
`~/Library/Application Support/agentre/`. `agentre-dev` is a sibling dir, parallel to the
existing `agentred` daemon dir (`paths.AppNameAgentred`).

### Why one function is enough

All desktop state flows through `paths.AppDataDir()`:

- `internal/bootstrap/cago.go` — `agentre.db`, cago config, and `LogsDir()` (`<dataDir>/logs`).
- `internal/pkg/agentruntime/cwd.go` — agent working copies.
- `internal/pkg/agentruntime/runtimes/piagent/session.go` — piagent session root.

Changing the resolver isolates all of them at once. No consumer changes needed.

### Dev-mode detection — single source of truth

`main.go` already has `isWailsDevMode()` reading `os.Getenv("devserver")` (the env var
`wails dev` sets in the binary it compiles+runs; today used only to disable the
single-instance lock). To avoid a second copy of that logic:

- Add `paths.IsDevMode() bool` — reads/trims `devserver`. `paths` stays a leaf package
  (it already reads env vars).
- `main.go`'s `isWailsDevMode()` delegates to `paths.IsDevMode()`.

### Single-instance lock

`singleInstanceUniqueID(dataDir)` derives from the data dir and is already **disabled in
dev mode**. With a separate dev dir: prod's lock ID is unchanged, and dev (lock disabled)
never contends. The two run concurrently. No change required here beyond what falls out of
the new path.

## Visual marker (secondary)

Data isolation is the priority; this is a small convenience kept because both builds will
often be open at once. When `paths.IsDevMode()`, set the window title to `Agentre (Dev)`
instead of `Agentre`, in `newWailsOptionsForDataDir` (`main.go`). Droppable without
affecting isolation.

## Tests (TDD — written first, watched to fail)

`internal/pkg/paths/paths_test.go`:

- `IsDevMode()` true when `devserver` set, false when empty/unset.
- `AppDataDir()`: `devserver` set + `AGENTRE_DATA_DIR` empty → `<base>/agentre-dev`.
- `AppDataDir()`: `AGENTRE_DATA_DIR` set wins even with `devserver` set.
- `AppDataDir()`: neither set → `<base>/agentre` (regression guard for prod path).

Existing `paths_test.go` cases must continue to pass; new cases explicitly set/clear
`devserver` via `t.Setenv` to avoid cross-test leakage.

Title marker: `newWailsOptionsForDataDir` is already split out for testability (takes
`goos`). If feasible without disproportionate plumbing, assert the title flips under dev
mode; otherwise the data-path tests are the contract that matters.

## Out of scope

- claude-code / codex's own `~/.claude` (and equivalent) state — their dirs, not agentre's.
- The `agentred` daemon — already isolated via `paths.AppNameAgentred`.
- Migrating/copying existing prod data into the dev dir — dev starts clean (intended).
