# Architecture

Agentre's code organization, layering conventions, remote execution architecture, and persistence layout. Read this before writing new code.

## Project layout

```text
main.go                        (wails bootstrap + `agentre claudecode …` CLI shim)
cmd/agentred/                  (headless daemon binary — root.go / run.go / pair.go / status.go / client.go)
internal/
  app/                         (Wails bindings — one file per domain: app.go / agent.go / chat.go / project.go / …,
                                methods only do parse → svc.Xxx().Method(ctx, …) → return)
  bootstrap/                   (startup order: dataDir → cago memory config → logger → SQLite → migrations)
  cli/claudecodecmd/           (CLI subcommands of the agentre binary, invoked by hook subprocesses)
  daemon/                      (agentred-side daemon: client / handlers / sessions / pairing / rpc / remotefs / notifier / state)
  service/<domain>_svc/        (business logic; interface + singleton accessor + private implementation)
  repository/<domain>_repo/    (data access; interface + Register/accessor, uniformly going through db.Ctx(ctx))
    mock_<domain>_repo/        (mockgen output, injected into service unit tests)
  model/entity/<domain>_entity/(rich domain entity; GORM tag + business methods)
  pkg/                         (cross-cutting internal packages: agentprovider / agentruntime / agentskill / agenttool / ccoauth /
                                claudecodehook / clienv / cliprober / cliprocess / code (i18n error codes) / diff / httpgateway /
                                jsonrpc / keychain / llmcatalog / paths / procattr / pty / remotefs / sysnotify)
  buildinfo/                   (CommitID ldflag target)
migrations/                    (gormigrate sequential migrations, filename prefix YYYYMMDDNNNN)
pkg/                           (externally reusable packages: claudecode / codex / piagent —— independently maintained CLI subprocess wrappers;
                                agentred/protocol —— shared agentred wire protocol)
frontend/                      (React 19 + TS + Vite + Tailwind; wailsjs/ is wails-generated, gitignored)
```

The `App` struct lives in `internal/app/app.go` (lifecycle + common methods), with domain methods spread across sibling files (`agent.go`, `chat.go`, …). **Keep these bindings thin — logic inside `App` is unreachable from `go test`; always put business logic in `service/`.**

## Remote execution (remote chat)

The desktop app can dispatch a single chat to an `agentred` daemon on the LAN for execution:

```
UI
  → internal/app Wails binding
  → chat_svc
  → internal/daemon/client (JSON-RPC client)
  → agentred (internal/daemon/{rpc,handlers,sessions})
  → claude-code / codex subprocess
```

- Tool approval / ask-user-question are still rendered by the desktop UI.
- A disconnect aborts the entire chat.
- pairing / device status go through `internal/pkg/remotefs` + `remote_device_svc`.

## Layering conventions (cago framework style)

- **Entity (rich model)** — validation around a single entity (`Check(ctx)`), state checks (`IsActive()`), and field serialization (`GetXxx/SetXxx`) methods all live on the entity. The service only coordinates across entities and orchestrates external dependencies. **Do not cram all the rules into the service.**
- **Repository** — consumer-defined pattern: `type XxxRepo interface { ... }` + `func Xxx() XxxRepo` + `RegisterXxx(impl)`. Queries uniformly use `db.Ctx(ctx).…`. Transactions:

  ```go
  db.Ctx(ctx).Transaction(func(tx *gorm.DB) error {
      ctx = db.WithContextDB(ctx, tx)
      // … all repo calls inside the transaction transparently go through tx
  })
  ```

- **Service** — interface + singleton + private implementation; the service depends only on the repository interface (dependency inversion), which makes mockgen unit testing easy. Use `gogo.Go(func() error { … })` for background tasks, and **do not pass the request ctx into the goroutine.**
- **Error / i18n** — error codes are defined in `internal/pkg/code/code.go` (allocated in segments of 10000+), with copy in `zh_cn.go` / `en.go`; call `i18n.NewError(ctx, code.Xxx)` to return them; for the HTTP status use `i18n.NewForbiddenError` / `i18n.NewErrorWithStatus`.
- **Wails binding layer** — methods in `internal/app/*.go` only do: parse → call `svc.Xxx().Method(ctx, …)` → return. **Do not stuff business logic into the App struct**, otherwise go test will not cover it. Open a new file for each new domain.
- **Entity over hardcoding** — use persisted fields (type/status/icon/color/config) as the source of truth, avoiding hardcoded default values in the service that bypass the entity.

## Storage and paths

All desktop persistence is centralized in **AppDataDir**:

| Platform | AppDataDir                                              |
| ------- | ------------------------------------------------------- |
| macOS   | `~/Library/Application Support/agentre/`                |
| Windows | `%LOCALAPPDATA%\agentre\`                               |
| Linux   | `~/.config/agentre/`                                    |

Use `AGENTRE_DATA_DIR` to override during testing or troubleshooting.

```text
<AppDataDir>/
  agentre.db          ← SQLite business database (gorm + gormigrate)
  logs/
    agentre.log       ← full log (info+, dropped to debug+ in DebugMode)
    error.log         ← error+ only
```

- **Business data** → SQLite, via `internal/repository/*_repo`.
- **cago runtime config** → in-memory source (`configs.WithSource(...)`), **not persisted to `config.json`**.
- **Frontend experience preferences (theme, window size, etc.)** → browser localStorage. Existing keys include `agentre.theme`, `agentre.windowSize`, `agentre.lastPath`.
- **agentred** uses a separate directory `agentred`, which can be overridden with `AGENTRED_DATA_DIR`.

Environment variables:

- `AGENTRE_ENV` — `dev` / `test` / `pre` / `prod`, defaults to `dev`.

Debug logging no longer goes through an environment variable: it is controlled by the "Settings → Version & Updates → Debug logging" toggle, persisted as `logger.debug_enabled` in the `app_settings` table; toggling it hot-reloads the logger (takes effect immediately, no restart needed), and on startup it is restored from the persisted value.

## Database and migrations

- Driver: pure-Go SQLite (`github.com/glebarez/sqlite`, no CGO required), registered to cago's `db` component via the anonymous import `_ "github.com/cago-frame/cago/database/db/sqlite"`.
- Initialization is handled uniformly by `internal/bootstrap.Init`: register the `db.Database()` component → call `migrations.RunMigrations(db.Default())`. At runtime, get the database via `db.Ctx(ctx)`, and use `db.WithContextDB(ctx, tx)` for transactions spanning functions.

Adding a migration:

1. Create `YYYYMMDDNNNN_xxx.go` under `migrations/`, exporting `migrationYYYYMMDDNNNN() *gormigrate.Migration`.
2. Append it to the **end** of `migrationList()` in `migrations/migrations.go`. **Do not modify existing migrations**; add a patch migration when a fix is needed.
3. Prefer raw SQL for DDL (`tx.Exec(…)`), and do not rely on the implicit behavior of `AutoMigrate`.

## Generated / self-managed files

| Path                                | Producer                                 | Regenerate                  |
| ----------------------------------- | ---------------------------------------- | --------------------------- |
| `frontend/wailsjs/**`               | Wails (from `App` bindings + Go structs) | `make dev` / `wails build`  |
| `internal/**/mock_*/`               | `mockgen`                                | `make mock`                 |
| `frontend/dist/`                    | Vite (embedded via `//go:embed`)         | `wails build`               |
| `<AppDataDir>/agentre.db`           | gorm + gormigrate                        | auto-migrated on startup    |
| `<AppDataDir>/logs/*.log`           | cago logger                              | rolled at runtime           |

Lockfiles —— never hand-edit; use `go mod tidy` / `pnpm add|remove|install`. `frontend/wailsjs/` is gitignored.
