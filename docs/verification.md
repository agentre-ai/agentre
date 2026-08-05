# Feature Verification and Report Discipline

> **What this owns.** Confirming a change really works — or that a bug really reproduces — by
> driving the **real running app**, and what that run has to leave behind: one `report.md` per
> scenario, with evidence chosen by what was verified, and a verdict written honestly.
>
> It does **not** own the harness. How the e2e harness is built, how to run it, how to write a
> spec, and when to promote one into the committed suite all live in
> [../e2e/README.md](../e2e/README.md) — its §6 points here for the ad-hoc verification workflow
> and evidence discipline. Test design and the test stack are [testing.md](./testing.md)'s; Fix Discipline is [develop.md](./develop.md)'s;
> log / SQLite investigation is [debugging.md](./debugging.md)'s.

## When This Applies

Driving the real app is the **last** check, not the first, and not every change needs it. The
criterion: **does the behavior depend on cross-process wiring — real Wails IPC, the real service /
repository / SQLite chain, a real CLI subprocess or gateway round-trip — that a unit test cannot
cover?**

Skip it when the answer is no:

- Pure logic already covered by a targeted test (parsers, reducers, translators, `pkg/*` protocol
  decoding) — **write and run that test instead**; it is faster and it stays.
- Docs, comments, types.
- Anything the committed suite already proves.

**This is process routing, not an exemption from TDD.** A scratch reproduction is the "confirm the
bug exists" step from [AGENTS.md](../AGENTS.md)'s Fix Discipline — it is **not** the regression test
that rule requires. Reproduce it in scratch if you must, then still write the failing committed
test before patching.

## Clear the Cheap Signals First

In proportion to what changed, not mechanically:

```bash
go test -race -run TestName "./internal/service/${domain}_svc/..."   # the tests relevant to this change
cd frontend && pnpm test -- path/to/file.test.tsx
cd frontend && pnpm exec tsc -b --noEmit                      # vitest does not check types
make test-backend                                             # wide surface / shared code only
```

**Green unit tests do not mean the feature works.** The fake runtime, real IPC, real migrations and
real gateway ports are only exercised at real runtime — that gap is what this guide fills.

## Write the Throwaway Spec

Same conventions as a committed spec — `data-testid` locators (never visible text, which is i18n'd
and brittle), Playwright's auto-wait rather than sleeps, and the `e2e/fixtures/db.ts` oracle. The
full list is [`../e2e/README.md`](../e2e/README.md) §5.

- **Needs a UI hook that doesn't exist?** Add a minimal `data-testid` to the component — additive
  and in scope for this task. No renaming sweep.
- **Surfaced a real bug?** Fix the **producer**, per [develop.md](./develop.md)'s Fix Discipline —
  and that fix still owes a committed regression test.

## Where the Evidence Goes

One scenario, one directory under `e2e/scratch/` — **the whole directory is gitignored**
(`.gitignore`: `e2e/scratch/*`, with only `README.md` negated), so the spec and its evidence sit
together and neither is ever committed:

```
e2e/scratch/<task-name>/
├── verify.spec.ts     # the throwaway spec (Playwright's testDir is recursive — it is picked up)
├── report.md          # the human-readable record — start reading here
├── logs/              # command output, the app log, the webServer log
├── resources/         # DB query output, exported files, captured payloads, before/after snapshots
└── screenshots/       # UI only
```

`<task-name>` is a lowercase hyphenated slug. **When the run is wrap-up acceptance against an
approved local spec in `docs/specs/`, `<task-name>` is that spec's own slug** (e.g.
`2026-07-31-subagent-model-badge`) — that is what keeps one round's evidence in one place.

**A directory with no `screenshots/` is not missing evidence.** A backend / daemon / migration
scenario normally holds only `report.md`, `logs/` and `resources/`.

Run just this scenario — same runner, same cleanup, so nothing about the harness changes:

```bash
cd e2e && pnpm run test:scratch "scratch/${task_name}/"
```

(The run commands themselves, and what the harness guarantees, are
[`../e2e/README.md`](../e2e/README.md)'s.)

**A quick look you delete in a minute does not need a directory** — a flat
`e2e/scratch/poke.spec.ts` is fine (that is the starter shape in
[`e2e/scratch/README.md`](../e2e/scratch/README.md)). The directory + report is required when the
run is **acceptance against a spec**, a **bug reproduction**, or **anything whose result you are
going to report to someone**.

### Create `report.md` Before Running

Copy the block under **Use This Shape** in [references/verification-report-template.md](./references/verification-report-template.md) into the scenario directory as `report.md` **before** starting the app, and fill it in as you go. Reconstructing it afterwards from
memory is where "I think it worked" comes from.

**Redact before saving, and again before embedding** — tokens, cookies, real credentials, the
contents of a real `~/Library/Application Support/agentre` DB.

## Choose the Evidence Form by What You Verified

Decisive evidence is **the smallest readable record that lets a reader re-check your conclusion**,
not "is there a picture":

| What was verified | Decisive evidence |
| --- | --- |
| UI / interaction | Screenshots for static state; a short recording **plus key still frames** when sequencing is the point |
| Chat / turn behavior | The `e2e-fake-reply: <prompt>` round-trip, plus the DB oracle — e.g. `runningSessionCount()` polling to `0` |
| Service / repository / IPC | The DB read back through `e2e/fixtures/db.ts` or a read-only query against `$AGENTRE_DATA_DIR/agentre.db` |
| Agent-backend events | The specific redacted event-kind / timing / stop-reason lines that decide it (enable Debug Logging in Settings → Version & Updates); never attach complete frames |
| Migration | Forward command + exit code **against a DB holding real existing rows**; rollback command when supported, otherwise restore/backup evidence plus the explicit no-down rationale; the same query before and after, side by side |
| CLI / daemon command (`agrctl` / `agentred`) | Full command + exit code + the stdout/stderr lines that decide it |
| Pure logic | The test run: command + exit code + assertion output |

**Assert the side effect independently, not just the UI.** "The UI updated" misses a failed write —
that is precisely the "stuck running / lost status write" class of bug. The DB oracle is
independent of the app's own service layer, which is what makes it worth reading.

## Report Honestly

- **It works** → say what you ran and what you observed: the command, the assertion value, the
  screenshot, the report path. **"No errors" is not evidence.**
- **A failure, or a path nobody reached** → say so plainly, with the raw output. Do not soften it,
  and do not claim a success you did not see.
- **Reproducing a bug** → state which of the two the scratch asserts, because they look identical
  in a pass/fail summary and mean opposite things:
  - the **expected** behavior — stays **red**, showing the gap; or
  - the **current buggy** contract — **green**, annotated that it must flip once fixed.

  **Never describe red as green.**
- **Never weaken an assertion or delete a check to make a scratch run pass.**

Anything short of every criterion holding **cannot** be summarized as holding: `not observed` means
nobody reached a decisive observation; `does not hold` means someone looked and saw it fail. Neither
is something a single word absorbs.

## Wrapping Up Acceptance Against a Spec

When the round works from an approved local spec in `docs/specs/`, the report additionally carries
**one verdict line per acceptance criterion** — `holds` / `does not hold` / `not observed`, each
with how it was checked and a command the reader can run themselves.

**The verdicts live in one place only** (the template's "Verdict" table). The evidence sections hold
the *evidence*; restating the verdicts beside them creates a second copy, and the stale copy is
always the one that gets read. A criterion exercised only against the fake runtime does not make a
real-integration criterion `holds` — write `not observed` and say what the fake did establish.

## Maintaining This File

The paths and commands above are checkable; when the harness moves, bring this file in line with
the branch (see [documentation.md](./documentation.md)):

```bash
git grep --cached -n 'e2e/scratch' -- .gitignore                 # the proposed scratch rule is staged
git grep --cached -n 'testDir' -- e2e/playwright.scratch.config.ts   # staged config still points at ./scratch
git grep --cached -n -A2 '^e2e-scratch:' -- Makefile             # staged run command still exists
git ls-files --cached --error-unmatch e2e/fixtures/db.ts docs/references/verification-report-template.md
```
