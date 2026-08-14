import { spawn } from "node:child_process";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const e2eRoot = resolve(here, "..");

export const GUARD_TESTS = [
  "lib/run-context.test.mjs",
  "lib/app-overlay.test.mjs",
  "lib/fake-sync-server.test.mjs",
  "lib/target.test.mjs",
  "lib/current-contract.test.mjs",
];

export function runNodeGuards({ cwd = e2eRoot, spawnProcess = spawn } = {}) {
  return new Promise((resolveRun, reject) => {
    const child = spawnProcess(process.execPath, ["--test", ...GUARD_TESTS], {
      cwd,
      stdio: "inherit",
    });
    child.once("error", reject);
    child.once("exit", (code, signal) => {
      if (code === 0) {
        resolveRun();
        return;
      }
      reject(
        new Error(
          signal
            ? `runner isolation tests terminated by ${signal}`
            : `runner isolation tests failed with exit ${code ?? 1}`,
        ),
      );
    });
  });
}
