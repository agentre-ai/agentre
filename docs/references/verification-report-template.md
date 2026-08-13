<!-- Copy into the scenario directory as report.md before running. Headings stay English; write the record in the user's language. Delete unused sections and this comment. -->

# Local verification: <scenario>

## Mode

`verifying a change` | `reproducing a bug`

## Goal / problem

<Expected observable behaviour and risk, or Expected/Actual bug statement>.

## Environment

<!-- What the run drove, so a reader can tell a hermetic run from a keep-alive one. -->

- Form and entry point: `<binary / driven app (make verify-up + drive.mjs) / scratch spec / make e2e-sync>`
- Data directory and devserver: `<$AGENTRE_DATA_DIR and port, never the real ~/Library path>`
- Build under test: `<commit sha, and the flavor: fake = -tags e2e deterministic runtime, real = real CLI runtimes>`

## Verdict

<!-- Fill last. Keep verdicts only here. One row per criterion — split a compound one rather than averaging it. Where `not observed` came from unconfigured environment, "How observed" names the service and the absent variable names, never values. -->

| # | Criterion / bug claim | Verdict | Real / substituted | How observed | Check it yourself |
|---|---|---|---|---|---|
| V1 | `<copied verbatim from the spec>` | holds / does not hold / not observed | real, or `substituted: <what stood in> — <what it does not cover>` | `<the runtime observation that decides it>` | `<command, or launch command plus steps>` |

Summary: <what holds, the deciding observation, every not-observed/failed item and shipping implication>.

| Label | Use it when | Requires |
|---|---|---|
| `holds` | you observed the behaviour at runtime | the deciding observation, and how a reader reaches it |
| `does not hold` | you observed it failing, or the bug reproducing | the failing output, assertion diff or error screenshot |
| `not observed` | you never reached the check | what stopped it |

An unreached check is never `holds`; a run that verified two of three criteria is reported as two of three.

## Authorization

<!-- Keep only when a real dependency was substituted or an external effect was authorized. The build-tag fake runtime is a substitution: name what it does not establish, such as a real claude-code subprocess. -->

| # | Substitute or effect | The user's authorization, verbatim |
|---|---|---|
| V1 | `<what stood in for what, or the effect and what it touches>` | `<sentence>` |

## Reproduction steps

<!-- Keep for bug reproduction; state whether the assertion encodes the expected behaviour (stays red) or the current buggy contract (green until the fix flips it). Include the smallest input that triggers it. -->

1. `<clean-checkout-to-observation steps>`

## Acceptance evidence

<!-- One `###` per verdict row, holding everything that decides it in the order observed. No verdict labels here. A row with no section is `not observed`. -->

### V1 · `<the criterion, verbatim>`

```console
$ <command>   # cwd and relevant redacted environment
<deciding lines>
$ echo $?
0
```

<What this proves>. Full output: `logs/<file>`.

**Independent side-effect check.** <The DB read back through `fixtures/db.ts` or a read-only query against the temp `agentre.db` — asserting the UI updated is necessary but not sufficient, and a failed write behind a cheerful UI is exactly what this catches.>

```text
<the row, count or event line, redacted>
```

<!-- UI only; pair before/after or light/dark in one table so the comparison is one glance. A recording carries a verdict only alongside the stills captured during the run. -->

| Before | After |
|---|---|
| `![before](screenshots/v1-before.png)` | `![after](screenshots/v1-after.png)` |

## Evidence index

- Commands/logs: `<inline deciding output plus optional full-file links; a driven run's action ledger is logs/drive.log>`
- Resources/data snapshots: `<paths and what each proves>`
- Screenshots/video: `<UI only; video includes decisive stills>`

A scenario with no `screenshots/` is not missing evidence — a backend, daemon or migration run normally holds only `report.md`, `logs/` and `resources/`.

## Persistent data changes

<!-- Keep only when the run wrote data that outlives it — a real server, a migration against real rows. The e2e temp data directory is not one. -->

| Change | Forward | Backward/backup | Before/after query |
|---|---|---|---|
| `<scope/blast radius>` | `<command/exit>` | `<command/exit or irreversible plan>` | `<evidence>` |

Dataset: `<source, size and representative edge values>`. Compatibility window: `<old/new readers>`.

## Execution record

| Step | Status | Evidence/blocker |
|---|---|---|
| `<step>` | pending / passed / failed / blocked | `<path or observation>` |

## Integrity and cleanup

- Initial/final HEAD: `<sha>` / `<sha>`
- Final `git status --porcelain=v1`: `<output>`
- Created artifacts, processes and external data, and how each was cleaned up: `<inventory>`
- Redaction performed: `<what was removed>`

## Evidence rules

- Every `holds` names how the target was driven — command, or launch command plus steps — and the deciding observation.
- Where a criterion changes state beyond the driven surface, that observation is an independent read with its own command; capture it **while the run is alive** — `make e2e` / `make e2e-scratch` delete the temp DB and logs after a passing run, and `make verify-down --wipe` removes them on request.
- Embed decisive text and images inline; scrolling this file should reach a verdict without opening a side file. Link only archives, binaries and full captures, each with a note on what it holds.
- Paste terminal output as text. For agent-backend behaviour, paste the specific event-kind, timing or stop-reason lines that decide it — never complete frames.
- Keep failed and unchecked steps visible. Redact tokens, cookies, real credentials and the contents of a real `~/Library/Application Support/agentre` DB before saving, and again before embedding.
- Keep every path relative to this file; the scenario directory, not `report.md` alone, is what you hand to a reviewer.
