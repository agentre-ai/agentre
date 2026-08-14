<!-- Copy into e2e/scratch/<scenario>/report.md before running. Headings stay English; write the record in the user's language. Delete unused sections and this comment. -->

# Local verification: <scenario>

## Mode

`verifying a change` | `reproducing a bug`

## Goal / problem

<Expected observable behaviour and risk, or Expected/Actual bug statement>.

## Environment

- Form and entry point: `<formal binary / formal desktop (make verify-up + drive.mjs)>`
- Isolated data/keychain and bridge: `<paths and port printed by make verify-status; never installed/development state>`
- Build under test: `<commit sha>`
- Real dependencies configured: `<Server / daemon / agent CLI / none; names and versions, never secrets>`

## Verdict

<!-- Fill last. Keep verdict labels only here. An unavailable real dependency is failed or not observed, never silently substituted. -->

| # | Criterion / bug claim | Verdict | Dependency actually exercised | How observed | Check it yourself |
|---|---|---|---|---|---|
| V1 | `<copied verbatim from the spec>` | holds / does not hold / not observed | `<real local desktop, Server, daemon, CLI, platform>` | `<deciding runtime observation>` | `<command, or launch command plus steps>` |

Summary: <what holds, the deciding observation, every failed/not-observed item and shipping implication>.

| Label | Use it when | Requires |
|---|---|---|
| `holds` | the behavior was observed at runtime | deciding observation and reproducible route |
| `does not hold` | it failed or the bug reproduced | failing output, assertion diff, or screenshot |
| `not observed` | the check was never reached | exact blocker, including missing dependency/configuration names but no values |

## Authorization

<!-- Keep when the run creates an external/destructive/paid effect. Record authorization before execution. -->

| # | External or destructive effect | Scope and cleanup | The user's authorization, verbatim |
|---|---|---|---|
| V1 | `<effect>` | `<systems/data touched and cleanup>` | `<sentence>` |

## Reproduction steps

<!-- Keep for bug reproduction; state whether the committed assertion encodes expected behavior or current buggy behavior. -->

1. `<clean-checkout-to-observation steps>`

## Acceptance evidence

<!-- One ### per verdict row. A row without deciding evidence is not observed. -->

### V1 · `<criterion, verbatim>`

```console
$ <command>   # cwd and relevant redacted environment
<deciding lines>
$ echo $?
0
```

<What this proves>. Full output: `logs/<file>`.

**Independent side-effect check.** <A read-only database/status/peer observation not shared by the driven surface.>

```text
<row, count, status, or redacted event line>
```

<!-- UI only. Pair comparisons in one table. -->

| Before | After |
|---|---|
| `![before](screenshots/v1-before.png)` | `![after](screenshots/v1-after.png)` |

## Evidence index

- Commands/logs: `<inline deciding output plus optional full-file links; driven actions are in logs/drive.log>`
- Resources/data snapshots: `<paths and what each proves>`
- Screenshots/video: `<UI only; video includes decisive stills>`

A scenario without screenshots is valid for backend, daemon, CLI, or migration verification.

## Persistent data changes

<!-- Keep only when the run writes data outside the isolated verification directories. -->

| Change | Forward | Backward/backup | Before/after query |
|---|---|---|---|
| `<scope/blast radius>` | `<command/exit>` | `<command/exit or irreversible plan>` | `<evidence>` |

Dataset: `<source, size, representative edge values>`. Compatibility window: `<old/new readers>`.

## Execution record

| Step | Status | Evidence/blocker |
|---|---|---|
| `<step>` | pending / passed / failed / blocked | `<path or observation>` |

## Integrity and cleanup

- Initial/final HEAD: `<sha>` / `<sha>`
- Final `git status --porcelain=v1`: `<output>`
- Created artifacts, processes, isolated state, and external data; cleanup/retention for each: `<inventory>`
- Redaction performed: `<tokens, cookies, credentials, personal paths, complete frames, real DB contents removed>`

## Evidence rules

- Every `holds` names how the formal target was driven and the deciding observation.
- An unavailable Server, daemon, CLI, platform integration, or authorization is failed/blocked/`not observed`, never replaced with a fake.
- Where a criterion changes state beyond the driven surface, include an independent read with its own command.
- Embed decisive text and images inline; link only archives, binaries, or complete captures and say what each contains.
- Paste terminal output as text. For agent behavior, include only deciding redacted event-kind, timing, or stop-reason lines — never complete frames.
- Keep failed and unchecked steps visible. Redact tokens, cookies, real credentials, sibling configuration, and installed/development database contents before saving and embedding.
- Keep paths relative to this file; the scenario directory, not `report.md` alone, is the review artifact.
