# AGENTS.md

This file provides unified guidance for all AI coding agents (Claude Code, Codex, etc.) working in this repository.

## Repository Facts

- Agentre is a Wails v2 desktop app: Go 1.26 backend + React 19 / TypeScript frontend.
- Main tech stack: Go 1.26, Wails v2, React 19, TypeScript, Vite, Tailwind CSS v4, pnpm 10.33.
- The Go module path is `agentre`, with no VCS prefix — do not invent a `github.com/...` import prefix.
- Frontend-backend IPC only goes through the Wails bindings in `internal/app`; the generated bindings live in `frontend/wailsjs`; **do not add HTTP-style app APIs**.

This repository produces two binaries:

- **`agentre`** (root `main.go`) — the desktop app. It also doubles as a CLI shim: `agentre claudecode …` short-circuits to `internal/cli/claudecodecmd` before booting wails/cago (used by Claude Code hook subprocesses).
- **`agentred`** (`cmd/agentred/`) — a headless daemon that executes claude-code / codex subprocesses on behalf of a paired desktop over JSON-RPC-over-WebSocket on the LAN. The daemon-side handlers live in `internal/daemon/`.

## High-Priority Constraints (mandatory, non-negotiable)

The following are hard rules. If the current task conflicts with them, **stop and ask the user first** — do not work around them on your own.

1. **Strict TDD / BDD: Red → Green → Refactor, no exceptions.**
   - For a new feature, first write a BDD-style behavior spec (`Given … When … Then …` or goconvey `Convey("when X, then Y")`) covering the happy path plus at least one boundary/error case, **then write the implementation**.
   - Do not write implementation code without a failing test. See [docs/development.md](docs/development.md) for details.
2. **Verify the bug exists before fixing it.**
   - Write a regression test that reproduces the failure, **run it and watch it fail** (and fail for the right reason), then start patching.
   - If the bug genuinely cannot be reproduced in a test, **tell the user explicitly**, and then discuss the patch approach. Do not silently "this is probably how to fix it" and change code.
3. **Prefer refactoring over patching — fix the root cause, don't mask it.**
   - Fix the bad value the producer emits, instead of adding an `if x == nil` fallback guard at every consumer; don't repeatedly normalize the same field at multiple call sites — normalize once at the boundary.
   - A comment like `// workaround because X returns Y` is a smell; the code underneath most likely needs to change. Refactor bad structure away when you can rather than piling on patches — but keep the refactor **within the scope of the current task** and don't let it spill over.
4. **Do not modify files unrelated to the current task.**
   - The diff should only touch the producer + its tests, plus at most one obvious in-scope drift.
   - **No** drive-by refactor / rename sweep / formatter pass / dead-code cleanup / import reordering / unrelated test churn — they bury the real change and break `git bisect`.
   - When you see unrelated dirty data, flag it to the user and ask, **do not fix it in passing**.
5. **New visible frontend UI copy must go through i18n.**
   - New UI text uses `react-i18next`'s `t(...)`, and updates both `frontend/src/i18n/locales/zh-CN/common.json` and `frontend/src/i18n/locales/en/common.json`.
   - Do not add hardcoded Chinese; ESLint, via `eslint-plugin-i18next`'s `i18next/no-literal-string`, blocks hardcoded Chinese UI copy in JSX text and visible attributes.
   - Static `t("...")` keys and locale coverage are validated by `frontend/src/__tests__/i18n.test.ts`; run the relevant tests when you change copy.
   - Do not translate dynamic output such as agent / user / terminal / code / markdown; it naturally never enters `t(...)`, and using a global text-rewrite fallback is forbidden.
   - All static UI copy is explicitly `t(...)`. See [docs/frontend.md](docs/frontend.md) for details.

## SOLID (coding rules)

Run every new package / type / function through this before merging:

- **S — Single Responsibility (SRP).** One reason to change is enough. If a service does parsing + persistence + notification at once, split it; a function that runs hundreds of lines almost certainly violates SRP.
- **O — Open/Closed (OCP).** Extend by adding a new type / strategy, not by editing a switch scattered across multiple callers. A new agent backend (Claude Code / Codex / built-in) should be an interface implementation, not a patch to `if engine == ...`.
- **L — Liskov Substitution (LSP).** An implementation must hold the interface contract (nil semantics, error types, side-effect boundaries); there should be no surprise like "this implementation ignores ctx".
- **I — Interface Segregation (ISP).** Define interfaces on the **consumer** side and narrow them as needed: a service declares only the few repo methods it uses, instead of making the consumer depend on a fat 20-method interface.
- **D — Dependency Inversion (DIP).** A `service` depends on a repository **interface**, never on the concrete implementation; the concrete implementation is wired up in `main.go` / `internal/app` via `RegisterXxx(impl)` — which is exactly what makes TDD mocking possible.

## High cohesion, low coupling (coding rules)

**High cohesion** — one set of packages per domain (`<domain>_entity` / `<domain>_repo` / `<domain>_svc`); open a new package for a new domain, and don't stuff unrelated functionality into a vaguely related old package. Put single-entity validation / state / serialization in the rich domain entity (`Check` / `IsActive` / `GetXxx`); the service only does cross-entity coordination and external-dependency orchestration, without piling on rules. Wails bindings are one file per domain (`internal/app/<domain>.go`), and methods only parse → `svc.Xxx().Method` → return. Put cross-cutting concerns in `internal/pkg/<concern>` (each package single-responsibility and self-contained), not scattered into domains.

**Low coupling** — dependencies flow one way: `internal/app → service → repository → model/entity`; `internal/pkg` is a leaf cross-cutting layer, referenced by every layer but **never reverse-importing** service / repository (currently zero reverse dependencies — hold that line). A service depends only on the repository **interface** (DIP); the implementation is wired up via `RegisterXxx(impl)` in bootstrap/main — this is the prerequisite for mock unit tests. Cross-package collaboration only goes through accessors (`xxx_repo.Xxx()` / `xxx_svc.Default()`); do not `new` someone else's implementation or directly `db.Ctx` into another domain's tables. Don't skip layers: `internal/app` does not touch repository / db, and a service does not bypass its own repo to hand-write raw SQL. Frontend-backend only goes through the Wails binding (no HTTP-style app API added), and remote-execution details are locked behind the `internal/daemon/client` interface.

> The above are the key points; for more concrete examples see [docs/development.md](docs/development.md), and for layering and dependency direction see [docs/architecture.md](docs/architecture.md).

## Development conventions (required reading)

Before writing code / fixing bugs / writing tests, read these docs first — the rules are in them:

- [docs/architecture.md](docs/architecture.md) — repository layout, cago layering conventions (entity / repo / service / wails binding), remote-execution architecture, the `AppDataDir` storage path, the `AGENTRE_DATA_DIR` / `AGENTRE_ENV` environment variables (Debug logging is now controlled by the "Settings → Version & Update" toggle), the database and migration flow, and the list of generated files.
- [docs/development.md](docs/development.md) — Red→Green→Refactor, SOLID, high cohesion / low coupling, Fix Discipline, the test stack (`testutils.Database(t)` + sqlmock + mockgen + goconvey), commit style, logging conventions, and `.golangci.yml` exceptions.
- [docs/frontend.md](docs/frontend.md) — the mandatory shadcn `@/components/ui/*` convention, pnpm, formatting / lint (`make lint` / `gofmt` / `goimports`), commit style, and the module path.
- [docs/debugging.md](docs/debugging.md) — sqlite3 / jq / log-filtering commands, the table-to-feature mapping, the command checklist for reproducing production bugs, and common pitfalls (on macOS the `Application Support` path must be quoted).
- [docs/agent-backend.md](docs/agent-backend.md) — the full path for wiring up a new AI agent backend (entity / migration / runtime / translator / capability / daemon import / frontend gating), including the TDD test checklist and common anti-patterns.
- [docs/doc-maintenance.md](docs/doc-maintenance.md) — required reading before changing any contributor doc (`AGENTS.md` / `CLAUDE.md` / `docs/*`): git-aware fact-checking, fixing or deleting stale facts directly (leaving no deprecation comments), doc organization rules, and the one-command verification script.

> See the cago skill (`/cago`) for details — complete controller / service / repo / cron / queue unit-test examples.

## Key constraints (essential facts)

- **The Wails binding layer only does parse → svc.Xxx().Method → return**; business logic stuffed into the `App` struct will be missed by go test.
- **Fixing a bug must start with a failing regression test**; do not add a guard at the consumer to mask a producer bug; do not smuggle a drive-by refactor / formatter pass into the same commit.
- **Repository unit tests always use `testutils.Database(t)` + sqlmock**; spinning up a real SQLite is forbidden (the migrations themselves and `internal/bootstrap/cago_test.go` are the only exceptions).
- **Service unit tests** generate a repo mock via `mockgen` + inject it via `RegisterXxx`, and **do not connect to a DB**.
- **Append new migrations to the end of `migrationList()`**; modifying an existing migration is forbidden; prefer native SQL for DDL, avoid relying on `AutoMigrate`.
- **Critical flows must log**: use `logger.Ctx(ctx)`, with a lowercase `package.Method:` prefix in the message, and dynamic values passed through `zap.Xxx(...)` fields.
- **Frontend form controls uniformly use shadcn `@/components/ui/*`**; adding a native `<select>` is forbidden.
- **New visible frontend UI copy must go through i18n**: use `react-i18next`'s `t(...)` and `frontend/src/i18n/locales/{zh-CN,en}/common.json`, do not add hardcoded Chinese; `i18next/no-literal-string` blocks hardcoded Chinese UI copy in JSX. Do not introduce a side-channel text-rewrite mechanism. Do not translate dynamic content such as agent / user / terminal / markdown. See [docs/frontend.md](docs/frontend.md) for details.

## Common Commands

```bash
make install-deps     # pnpm install in frontend/
make dev              # wails dev — hot reload
make build            # wails build with version/commit ldflags (current platform)
make build-windows    # cross-build windows/amd64 (override via WINDOWS_PLATFORM=)
make run              # build and launch production app
make install          # build + install app bundle (macOS: /Applications/Agentre.app)
make generate         # wails generate module — refresh frontend/wailsjs/ bindings
make test             # backend race tests + frontend Vitest (runs `generate` first)
make test-backend     # Go race tests excluding /frontend/
make test-frontend    # wails generate + frontend Vitest
make test-cover       # coverage.out + coverage.html
make lint / lint-fix  # golangci-lint + frontend ESLint (runs `generate` first)
make check            # lint + test
make mock             # go generate ./... (go.uber.org/mock)
make clean            # rm build/bin frontend/dist coverage.*

# agentred daemon (remote execution box)
make agentred                # build local-platform binary → build/bin/agentred
make agentred-linux          # cross-build linux/amd64 (override via AGENTRED_GOOS/ARCH)
make agentred-deploy         # build linux + opsctl-cp + install (AGENTRED_TARGET= host)

# Focused tests
go test -race -run TestName ./internal/service/chat_svc/...
go test -race ./internal/repository/llm_provider_repo -run TestName
go test -race ./pkg/codex -run TestName
cd frontend && pnpm test -- path/to/file.test.tsx
cd frontend && pnpm install                  # pnpm is source of truth, not npm
```

> `go test ./...` in this repository will scan a Go package under `frontend/node_modules`; by default use `make test-backend` (which explicitly excludes `/frontend/`).
