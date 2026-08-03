# Local Verification Report Template

> Copy the block under **"Use This Shape"** to `e2e/scratch/<task-name>/report.md` **before**
> starting the app, and update it as you go. When to produce one at all, where the directory goes,
> and how to pick the evidence form are [verification.md](../verification.md)'s.

It exists so **a reader can judge whether the implementation is right**, which is why **evidence
goes inline rather than linked out**: scrolling top to bottom should show the decisive output, log
lines and pixels without opening a side file. A bare link is the fallback for what genuinely cannot
be embedded (archives, binaries, huge logs), and it **must** say what is in it.

Two uses, one shape. In runnable commands, replace the literal `TASK_NAME` with the scenario slug first:

| Scenario | Required | Delete |
| --- | --- | --- |
| Wrap-up acceptance against a `docs/specs/*` spec | "Verdict" table + "Acceptance evidence" | "Reproduction steps", "Minimal reproduction" |
| Ad-hoc check / reproducing a bug | "Reproduction steps", "Minimal reproduction", "Verdict" | "Acceptance evidence" |

"Persistent data changes" is kept **only** when the round touched migrations or existing rows.

## Use This Shape

````md
# Local verification record: <scenario name>

## Mode

`verifying a change` | `reproducing a bug`

## Goal / problem

- (verifying a change) What behavior should hold, and why it might not
- (reproducing a bug) **Expected:** … **Actual:** …

## Verdict

<one sentence to a short paragraph, written last: whether it holds, the one observation that
decides it, and what nobody observed. Say "nothing outstanding" explicitly when that is true —
silence and "nothing left" read identically.>

- (reproducing a bug) state which the scratch asserts: the **expected** behavior (stays red) or
  the **current buggy** contract (green, must flip once fixed)
- (wrap-up acceptance) one line per acceptance criterion, and **only here**:

  | # | Criterion | Verdict | How | Check it yourself |
  | --- | --- | --- | --- | --- |
  | V1 | `<copied verbatim from the spec>` | holds | drove the real app | `cd e2e && pnpm run test:scratch "scratch/TASK_NAME/"` |
  | V2 | `<…>` | not observed | needs a real claude-code subprocess; the run used the e2e fake | - |

## Reproduction steps

1. …

## Minimal reproduction

- The smallest spec / steps that trigger it (linking `resources/…`)

## Acceptance evidence

> Spec: `docs/specs/<slug>.md`

### V1 · "<the criterion, verbatim>"

Check it yourself:

1. `cd e2e && pnpm run test:scratch "scratch/TASK_NAME/"` — builds and starts the real app on `localhost:34216` with a temp data dir.
2. <the step that matters, and why this one rather than any other path>
3. <what you should see>

| Before | After |
| --- | --- |
| ![before](screenshots/v1-before.png) | ![after](screenshots/v1-after.png) |

Where a criterion did **not** hold, put a two-column table here (what the spec requires / what
actually happens) rather than a paragraph. Anything uncertain, unreached or half-run is
`not observed` under "Verdict" — **never write something unverified as `holds`.**

## Evidence index

Anything not tied to a single criterion, embedded and annotated with what it proves. Subheadings
follow the evidence actually collected — "Screenshots" appears only for a UI change.

### Commands and output

```console
$ cd e2e && pnpm run test:scratch "scratch/TASK_NAME/"
Running 1 test using 1 worker
  ✓  1 scratch/TASK_NAME/verify.spec.ts:7:1 › <test name> (4.2s)
  1 passed (12.5s)
$ echo $?
0
```

<one sentence on what this proves>. Full output: [run.log](logs/run.log)

### Independent side-effect check

Assert the side effect **inside the Playwright scenario**, while its child environment and temp DB still exist, using the read-only helpers in `e2e/fixtures/db.ts`:

```ts
await expect.poll(() => runningSessionCount()).toBe(0);
```

<paste the deciding assertion/output and explain what it rules out that the UI assertion alone could not — e.g. a status write that never landed. A successful run deletes the temp DB, so do not present a post-run `$AGENTRE_DATA_DIR` query as independently reproducible.>

## Persistent data changes

Paste the deciding rows inline, side by side; link the full capture only as supporting detail:

```console
BEFORE: <deciding rows / schema>
AFTER:  <deciding rows / schema>
```

| The change | Forward | Backward | Before/after |
| --- | --- | --- | --- |
| `<what changed>` | `<command>` exit 0 | `<command>` exit 0 | `<what the inline rows establish>` · [full capture](resources/before-after.txt) |

- **Which DB it ran against**: where it came from, how many existing rows, which edge values
  (NULL / empty / dirty). **Green on an empty database is not evidence.**
- **No down migration**: why it is irreversible, and whether the substitute backup was actually run.

## Task checklist

- [ ] Preconditions passed (targeted tests, typecheck)
- [ ] Built and started the real app
- [ ] Drove to the target behavior and confirmed a stable anchor
- [ ] Corroborated the side effect independently (DB oracle / logs), not just the UI
- [ ] Saved this run's decisive evidence in the form the change calls for
- [ ] Wrote the conclusion, and the verdicts, under "Verdict"

## Blockers

- None
````

## Filling-In Discipline

**"Verdict" sits near the top but gets filled in last** — reading order, not writing order. Three
things it must answer:

1. **Whether it holds** — the behavior holds, the bug reproduced, or where the round stands.
2. **What that rests on** — the one observation that decides it. A clause, not a section.
3. **What nobody observed** — every `not observed` criterion, named by id, and what a reader accepts
   by shipping anyway. **Say so when there are none.**

The conclusion is **not** a second pass through the verdict table and **not** the evidence — one
sentence to a short paragraph, or it becomes a second report that gets skimmed past.

> **Seven of the eight criteria hold; V6 is not observed.** The badge rendered from the real
> `chat_messages` row in the running app, and the session settled to `idle` in the DB. V6 needs a
> real codex subprocess, which the e2e fake replaces, so remote-backend parity was observed by
> nobody — that is what merging accepts.

**The checklist stays honest:** list the unchecked tasks from the start; tick a box only once the
command or assertion **actually** passed; a blocked step stays unticked with a specific entry under
"Blockers".

## Embedding Rules

- **Commands and output** — the full command (with cwd and key env vars), the exit code, and the
  deciding lines, in a `console` block, **complete enough to copy and re-run**. Fold the rest into
  `<details>` rather than dropping it, with the summary saying how many lines and which file.
- **A list checked line by line** — a `- [x]` / `- [ ]` task list, with **the failing line left in,
  unticked**. Tidying it down to what passed turns a gap into a silence.
- **Expectation and reality disagreeing** — a two-column table (required / actual), not a paragraph.
- **Screenshots** — `![caption](screenshots/….png)` plus **one sentence on what it proves**; pairs
  (before/after, light/dark) in a two-column table. **UI only** — screenshotting a terminal is worse
  than pasting the text.
- **Recordings** — mp4 / h264, no autoplay, no base64, relative paths. **Capture the decisive
  moments as stills during the run and put them next to it** — the stills carry the verdict.
- **Logs** — paste the deciding lines, then link the full capture (the app log under
  `$AGENTRE_DATA_DIR`, the webServer log at `$TMPDIR/agentre-e2e-webserver.log`). A link alone
  forces the reader to reconstruct which line mattered.
- **Redact before embedding** tokens, cookies and real credentials.
