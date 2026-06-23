# e2e/scratch — throwaway functional-verification specs

Drop one-off `*.spec.ts` here to verify a feature you just finished, end-to-end, against the
**real running app**. Everything in this folder **except this README is gitignored** — these
scripts are not committed; write, run, observe, delete.

This is the GUI counterpart of "verify by observing": drive the real app, then read the
observable side-effects (UI assertions + the temp DB + logs). Full workflow, conventions, and
gotchas: **[docs/e2e-harness-guide.md](../../docs/e2e-harness-guide.md)** (§6).

## Run

```bash
make e2e-scratch             # runs every e2e/scratch/*.spec.ts via the live harness
# or a single file (still through the runner, so cleanup happens):
cd e2e && pnpm run test:scratch scratch/<file>.spec.ts
```

Reuses the same harness as the committed suite: launches `wails dev -tags e2e` on port 34216
with a temp data dir + `AGENTRE_ENV=test`. A native Agentre window opens (expected). webServer
output → `$TMPDIR/agentre-e2e-webserver.log`; the app's logs + `agentre.db` are under the temp
data dir (`$AGENTRE_DATA_DIR`).

## Starter template

```ts
// e2e/scratch/verify-my-feature.spec.ts  (gitignored — delete when done)
import { test, expect } from "@playwright/test";
import { runningSessionCount } from "../fixtures/db"; // read-only node:sqlite oracle; add helpers as needed

test("my feature works end-to-end", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByTestId("new-chat-button")).toBeVisible();

  // 1. Drive the UI like a user (data-testid locators + auto-wait, no sleeps). Add a data-testid
  //    in the component if you need a stable hook (additive only).
  // 2. Assert the UI reflects the change (and the fake reply: /e2e-fake-reply: <prompt>/).
  // 3. Corroborate the side-effect independently — e.g. no session stuck running:
  // await expect.poll(() => runningSessionCount()).toBe(0);
});
```

If a flow proves to be **core and stable**, promote it: move the spec into `e2e/tests/`,
harden it, and commit (see the harness guide §5). Otherwise just delete it.
