import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { test } from "node:test";

import { createAppOverlay } from "./app-overlay.mjs";
import { REPO_ROOT, createRunContext } from "./run-context.mjs";

test("Given the dedicated app contract, when the runner prepares the Go overlay, then it adds only the deterministic failure branch inside the private run root", async (t) => {
  const run = await createRunContext();
  t.after(() => run.remove());

  const overlayPath = createAppOverlay(REPO_ROOT, run.runRoot);
  const overlay = JSON.parse(await readFile(overlayPath, "utf8"));
  const replacements = overlay.Replace;
  assert.equal(Object.keys(replacements).length, 1);
  const fakeRuntime = `${REPO_ROOT}/internal/pkg/agentruntime/runtimes/fake/runtime.go`;
  assert.ok(replacements[fakeRuntime].startsWith(run.runRoot));
  const source = await readFile(replacements[fakeRuntime], "utf8");
  assert.match(source, /e2e-runtime-fail:/);
  assert.match(source, /e2e-runtime-failure:/);
});
