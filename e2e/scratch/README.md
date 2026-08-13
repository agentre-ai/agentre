# e2e/scratch — one directory per verification scenario

Everything a local verification run leaves behind lands here, and **everything except this README
is gitignored** — evidence and throwaway specs are never committed.

The run itself is [docs/verification.md](../../docs/verification.md)'s (when driving the real app
is warranted, what `report.md` owes, how to report a reproduction honestly). The machine is
[e2e/README.md](../README.md)'s. This file only says what the directory looks like.

## Layout

```
e2e/scratch/<scenario>/
├── report.md        # created BEFORE the run, filled in as evidence arrives
├── logs/            # drive.log (the action ledger), app/webServer output, command output
├── resources/       # DB query output, exported files, captured payloads, before/after snapshots
├── screenshots/     # UI only
└── verify.spec.ts   # ONLY when a spec was warranted (replay / timing / concurrency)
```

`<scenario>` is a lowercase hyphenated slug; for acceptance against a spec in `docs/specs/`, it is
that spec's own slug. A directory with no `screenshots/` is not missing evidence — a backend,
daemon or migration scenario normally holds only `report.md`, `logs/` and `resources/`.

## Driving (the default — you author nothing)

```bash
make verify-up                              # once; FLAVOR=real for the real claude/codex CLIs
export AGENTRE_VERIFY_SCENARIO=<scenario>   # every drive call records into this directory

node e2e/drive.mjs snapshot                 # what is on screen, and how to address it
node e2e/drive.mjs click "testid=nav-settings"
node e2e/drive.mjs shot 01-settings
node e2e/drive.mjs sql "select count(*) from chat_sessions where status='running'"

make verify-down                            # VERIFY_FLAGS=--wipe to drop the isolated state
```

Without `AGENTRE_VERIFY_SCENARIO` the ledger goes to `_unscoped/` — fine for a look you delete in
a minute. The scenario directory and `report.md` are required when the run is acceptance against
a spec, a bug reproduction, or anything whose result you report to someone; copy the block under
**Use This Shape** in
[`docs/references/verification-report-template.md`](../../docs/references/verification-report-template.md)
into `report.md` **before** starting.

## When a spec is warranted

Only when the *sequence* must be replayed, or timing/concurrency is the contract. Then Playwright
picks it up recursively, and the fast loop reuses the app that is already up:

```bash
AGENTRE_E2E_REUSE=1 make e2e-scratch TASK="scratch/<scenario>/"
```

```ts
// e2e/scratch/<scenario>/verify.spec.ts  (gitignored — delete when done)
import { test, expect } from "@playwright/test";
import { runningSessionCount } from "../../fixtures/db"; // read-only node:sqlite oracle

test("my feature works end-to-end", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByTestId("new-chat-button")).toBeVisible();

  // 1. Drive the UI like a user (data-testid locators + auto-wait, no sleeps).
  // 2. Assert the UI reflects the change (and the fake reply: /e2e-fake-reply: <prompt>/).
  // 3. Corroborate the side-effect independently — e.g. no session stuck running:
  // await expect.poll(() => runningSessionCount()).toBe(0);
});
```

If a flow proves **core and stable**, promote it: move the spec into `e2e/tests/`, harden it, and
commit (see [`e2e/README.md`](../README.md) §5). Otherwise just delete it.
