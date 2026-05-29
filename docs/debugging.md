# Debugging Agentre

## Overview

Agentre stores everything under **AppDataDir**. To debug a runtime issue, read the SQLite database and the zap-formatted logs there — don't add prints, don't run the app blind.

| Platform | AppDataDir |
|----------|------------|
| macOS    | `~/Library/Application Support/agentre/` |
| Windows  | `%LOCALAPPDATA%\agentre\` |
| Linux    | `~/.config/agentre/` |

Override with env var `AGENTRE_DATA_DIR` (used in tests and isolated debug runs).

```text
<AppDataDir>/
  agentre.db          ← SQLite (pure-Go driver, no CGO)
  logs/
    agentre.log       ← all levels (info+; debug+ when Debug Logging is on)
    error.log         ← error+ only
```

On macOS the path has a space — **always quote it** (`"$HOME/Library/Application Support/agentre/agentre.db"`), otherwise commands silently target the wrong file.

## When to Use

- Bug report says "data disappeared / wrong value showed up" → check `agentre.db`
- Background job (cron/hook sync) misbehaving → grep `agentre.log` for the caller
- App fails to start / panics → start with `error.log`
- Migration suspected → list `migrations` table, compare against `migrations/`
- Validating a feature you just shipped → tail logs while exercising the app

**Don't use this guide for:** writing new code (use the layered conventions in CLAUDE.md), or debugging tests (those use sqlmock, not this DB).

## Quick Reference

```bash
# Resolve data dir (handles AGENTRE_DATA_DIR override)
DATA_DIR="${AGENTRE_DATA_DIR:-$HOME/Library/Application Support/agentre}"
DB="$DATA_DIR/agentre.db"
LOG="$DATA_DIR/logs/agentre.log"
ERR="$DATA_DIR/logs/error.log"

# DB — list tables, inspect schema, run query
sqlite3 "$DB" ".tables"
sqlite3 "$DB" ".schema chat_sessions"
sqlite3 -header -column "$DB" "SELECT id, name, engine FROM agents ORDER BY id DESC LIMIT 10;"

# Applied migrations (compare against files in migrations/)
sqlite3 "$DB" "SELECT id FROM migrations ORDER BY id;"

# Tail live logs (pretty-print JSON with jq)
tail -f "$LOG" | jq -c '{ts,level,caller,msg,error}'

# Recent errors only
tail -n 200 "$ERR" | jq -c '{ts,caller,msg,error}'

# Filter by package/caller
grep -F '"caller":"hook_svc' "$LOG" | tail -n 50 | jq -c .

# Filter by a chat session
jq -c 'select(.session_id == 42)' "$LOG"
```

## Table → Feature Map

| Table | What lives here |
|-------|-----------------|
| `agents`, `agent_backends` | Agent definitions + which CLI backend (builtin/claudecode/codex) |
| `chat_sessions`, `chat_messages` | Conversation history, tool calls, thinking blocks |
| `llm_providers` | Provider configs (OpenAI/Anthropic/etc.) |
| `hook_sources`, `hook_rules`, `hook_events` | Hook ingestion (e.g. email source) and dispatch |
| `app_settings` | UI/runtime prefs persisted by the app |
| `departments` | Org structure for the org-chart UI |
| `migrations` | gormigrate ledger — one row per applied migration id |

When debugging, start from the table closest to the feature, then follow FK-style id fields into adjacent tables. Schemas are not documented separately — use `.schema <table>` against the live DB.

## Log Format (zap JSON)

Every line is one JSON object. Common fields:

- `level` — `debug` | `info` | `warn` | `error`
- `ts` — RFC3339 with millis (`2026-05-17T13:10:35.009+0800`)
- `caller` — `<pkg>/<file>.go:<line>` (e.g. `hook_svc/email.go:251`) — **this is your fastest filter**
- `msg` — short English description
- `error` — present on `warn`/`error` lines; may be a localized i18n message
- ad-hoc fields (`source_id`, `agent_id`, `session_id`, …) — added at the call site

Tip: toggle **Settings → Version & Update → Debug Logging** to enable debug-level logging — much more verbose, only use while reproducing a specific bug. It takes effect immediately (logger hot-reload) and survives restarts; the state lives in `app_settings.logger.debug_enabled`.

## Common Scenarios

**"Chat lost its history"** → `sqlite3 "$DB" "SELECT id, agent_id, updated_at FROM chat_sessions WHERE id=<sid>;"` then count messages; cross-check `agentre.log` around the timestamp for the calling `chat_svc/...` line.

**"Hook sync keeps warning"** → grep `agentre.log` for `caller":"hook_svc`; pull `source_id` from the warn; inspect that row: `SELECT * FROM hook_sources WHERE id=<n>;`.

**"DB looks stale after pulling main"** → diff applied vs. expected migrations:
```bash
diff <(sqlite3 "$DB" "SELECT id FROM migrations ORDER BY id;") \
     <(grep -oE 'migration[0-9]{12}' migrations/migrations.go | sort -u)
```
Missing ids ⇒ relaunch the app to run `RunMigrations`; never hand-insert into `migrations`.

**"App won't start"** → read `error.log` last 50 lines first. Mostly `mkdir … file exists` or `database is locked` style messages from `agentre/main.go` and `bootstrap/`.

## Common Mistakes

- **Forgetting to quote the macOS path.** The space in `Application Support` makes `sqlite3 $DB` open an empty in-memory DB silently — always `"$DB"`.
- **Writing to the DB while the app is running.** SQLite holds a write lock; either close the app or use `BEGIN IMMEDIATE` and accept `database is locked`. Read-only is fine.
- **Editing rows directly to "fix" a bug.** That hides the producer-side bug (CLAUDE.md Fix Discipline §2). Reproduce, then fix the Go code + add a regression test against sqlmock.
- **Trusting `agentre.log` after a crash.** zap may buffer the last few lines. Prefer `error.log` for fatals, or turn on **Debug Logging** (Settings → Version & Update) and reproduce.
- **Greppping with single quotes on a JSON field.** `grep '"caller":"hook_svc'` works; `grep "caller":"hook_svc"` does not (shell eats the quotes). Use `-F` for fixed strings.
- **Confusing this DB with test DBs.** `make test` uses sqlmock / MySQL dialect in memory — it never touches `$DB`. Bugs you reproduce here are real runtime state, not test fixtures.
