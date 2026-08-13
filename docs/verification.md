# Feature verification

Confirming a change really works — or that a bug really reproduces — by driving the real running app, and what that run leaves behind. The harness itself is [`../e2e/README.md`](../e2e/README.md)'s; test design is [testing.md](testing.md)'s; log and SQLite investigation is [debugging.md](debugging.md)'s.

## When to skip this route

Use targeted committed tests alone when they fully observe the changed logic — pure logic, parsers, reducers, `pkg/*` protocol decoding, docs, comments, types, and anything the committed suite already proves. Use this route when the behaviour depends on cross-process wiring that a unit test cannot cover: real Wails IPC, the real service → repository → SQLite chain, a real CLI subprocess or gateway round-trip. It does not replace TDD — a reproduction is the "confirm the bug exists" step of [AGENTS.md](../AGENTS.md)'s Fix Discipline and still owes the committed failing test.

## The route

**Start one app, drive it, record as you go.** You do not author a spec to look at something once.

```bash
make verify-up                       # fake runtime: deterministic, no CLI subprocess, no auth
make verify-up FLAVOR=real           # real claude-code / codex CLIs, no e2e build tag
export AGENTRE_VERIFY_SCENARIO=<scenario>   # every drive call records into this scenario

node e2e/drive.mjs snapshot                       # what is on screen, and how to address it
node e2e/drive.mjs click "testid=nav-settings"
node e2e/drive.mjs shot 01-settings
node e2e/drive.mjs sql "select status, count(*) from chat_sessions group by status"
node e2e/drive.mjs logs 40

make verify-down                     # add VERIFY_FLAGS=--wipe to drop the isolated state too
```

1. Run the targeted tests and `cd frontend && pnpm exec tsc -b --noEmit`; run `make test-backend` only when the blast radius is not confirmed local or a gate requires it.

```bash
go test -race -run TestName "./internal/service/${domain}_svc/..."
cd frontend && pnpm test -- path/to/file.test.tsx
```

2. Bring up the target. `make verify-up` owns the isolated data directory, keychain directory, gateway port and bridge port — a real dependency is reached through those overrides, and configuration it lacks is asked for, not arranged around. Never hand-start an app against your own data directory to verify something.
3. Choose the cheapest form that observes the contract, and put everything it produces under gitignored `e2e/scratch/<scenario>/`:

   | To reach and observe the target | You author |
   |---|---|
   | an existing command or entry point suffices, and it neither depends on nor writes your own machine state | nothing — drive it yourself and read the oracle |
   | it needs a specific launch, isolated state or real-target configuration, and the observation is one-off | nothing — `make verify-up` is that launch; drive it with `drive.mjs` |
   | the sequence must be replayed, or timing/concurrency is the contract | a full asserting spec |

   This project:

   | Change lands in | Reach it with | You author | Oracle |
   |---|---|---|---|
   | CLI or daemon — `agrctl`, `agentred` | run the binary | nothing | full command, exit code, the deciding stdout/stderr lines |
   | GUI or IPC | `make verify-up` + `node e2e/drive.mjs …` | nothing | `drive.mjs sql` against the isolated `agentre.db`, plus the screenshots and `logs/drive.log` the run wrote |
   | real claude-code / codex behaviour | `make verify-up FLAVOR=real` + the same driver | nothing | the same, plus `drive.mjs logs` (enable Debug Logging in Settings → Version & Updates for raw frames) |
   | replay, timing or concurrency as the contract | a scratch spec on the harness, reusing the running app (`AGENTRE_E2E_REUSE=1 make e2e-scratch TASK="scratch/<scenario>/"`) | a full spec | the `e2e-fake-reply: <prompt>` round-trip **plus** the DB oracle — e.g. `runningSessionCount()` polling to `0` |
   | sync against a real server and a peer | `make e2e-sync` ([`../e2e/README.md`](../e2e/README.md#10-the-sync-suite-make-e2e-sync--a-real-server-and-a-simulated-peer)) | per that suite | that suite's assertions |
   | a migration | the forward command against a DB holding real existing rows | nothing | the same query before and after, side by side; rollback command, or restore evidence plus the explicit no-down rationale |

   In every form one observation comes from a path the driven surface does not share — the DB read back independently of the app's own service layer. Asserting the UI updated is necessary but not sufficient; a failed write behind a cheerful UI is exactly what the oracle catches.

4. Before running, create `report.md` from [references/verification-report-template.md](references/verification-report-template.md); update it as evidence arrives.
5. Record how the target was driven, exit codes where the form produces them, deciding runtime observations, gaps and shortest user reproduction steps. `drive.mjs` already appends every action and its outcome to `e2e/scratch/<scenario>/logs/drive.log`, and writes screenshots into `screenshots/` — the report cites those, it does not restate them.

A quick look you delete in a minute does not need a scenario directory; run without `AGENTRE_VERIFY_SCENARIO` and the ledger lands in `e2e/scratch/_unscoped/`. The directory and `report.md` are required when the run is acceptance against a spec, a bug reproduction, or anything whose result you report to someone.

For acceptance against a spec in `docs/specs/`, `<scenario>` is that spec's slug. Extract each acceptance criterion into one verdict row and evidence section. Verdict labels are `holds`, `does not hold`, `not observed`, and they live only in the Verdict table.

For bug reproduction, state whether the reproduction asserts expected behaviour (red until fixed) or current buggy behaviour (green until fixed), then turn it into the committed failing test Fix Discipline requires. Choosing a form that authors nothing does not remove that test.

## What the route guarantees, and what it refuses

The launcher and the driver enforce this — they are not conventions you have to remember:

- **Only a throwaway app is ever driven.** Each flavor has its own temp data directory; the installed app's root and your own `make dev` root are refused by name ([`../e2e/lib/target.mjs`](../e2e/lib/target.mjs) `assertIsolatedDataDir`).
- **Only that app's own origin is ever driven.** `drive.mjs` refuses any other URL, including the other flavor's port and `make dev`'s 34115.
- **The isolated keychain applies to both flavors.** `AGENTRE_KEYCHAIN_DIR` is not gated on the `e2e` build tag (`internal/bootstrap/keychain.go`), so a real-runtime run cannot write to your system keychain; an unusable directory fails startup rather than falling back.
- **The oracle is read-only.** `drive.mjs sql` accepts only `SELECT` / `WITH` / `PRAGMA` / `EXPLAIN`: a verification run observes state, it does not manufacture it.
- **Nothing takes over your screen.** The browser is headless and the app's native window is hidden right after boot (and again after each navigation, because the frontend re-shows it on mount). `make verify-up VERIFY_FLAGS=--headed` when you want to watch.
- **Every checkout is its own island.** Ports, data dir, keychain dir and session file are derived from the checkout's absolute path, so a second worktree can verify at the same time and neither can touch the other's state. Within one checkout only one flavor at a time — `wails dev` compiles into that checkout's `build/bin`, and the second launch would overwrite the binary the first is running.

Never weaken an assertion, skip a failed step or describe red as green. For background and runtime effects, use the specific redacted event-kind, timing or stop-reason lines that decide it; “no errors” is not evidence, and never attach complete frames. Obtain authorization before destructive or external side effects, and before substituting anything for a real dependency — `FLAVOR=fake` is a substitution, and a criterion reached through it names that in its verdict row along with what the fake does not establish.

Redact before saving, and again before embedding: tokens, cookies, real credentials, and the contents of a real `~/Library/Application Support/agentre` DB.

## Maintaining this route

Harness facts are owned by [`../e2e/README.md`](../e2e/README.md). Follow [documentation.md](documentation.md) after path or harness changes. What this route still owns:

```bash
git grep --cached -n 'e2e/scratch' -- .gitignore                     # evidence stays local
git grep --cached -n -A2 '^verify-up:' -- Makefile                   # the launch command still exists
git grep --cached -n 'assertIsolatedDataDir' -- e2e/lib/target.mjs   # the isolation guard still exists
cd e2e && pnpm run test:guards                                       # and still holds
git ls-files --cached --error-unmatch e2e/drive.mjs e2e/verify.mjs docs/references/verification-report-template.md
```
