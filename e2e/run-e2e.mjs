import { spawn } from "node:child_process";
import { createRequire } from "node:module";
import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { createAppOverlay } from "./lib/app-overlay.mjs";
import {
  ProcessSupervisor,
  REPO_ROOT,
  appEnvironment,
  changedDatabaseMetadata,
  createRunContext,
  playwrightEnvironment,
  preserveFailureArtifacts,
  productionAndDevelopmentRoots,
  snapshotDatabaseMetadata,
  spawnLogged,
  waitForURL,
} from "./lib/run-context.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);
const playwrightCli = require.resolve("@playwright/test/cli");
const artifactRoot = join(here, "artifacts");

function childResult(child) {
  return new Promise((resolve) => {
    child.once("error", (error) => resolve({ code: 1, signal: null, error }));
    child.once("exit", (code, signal) => resolve({ code: code ?? 1, signal, error: null }));
  });
}

async function runNodeGuards() {
  const child = spawn(
    process.execPath,
    ["--test", "lib/run-context.test.mjs", "lib/app-overlay.test.mjs"],
    {
      cwd: here,
      stdio: "inherit",
    },
  );
  const result = await childResult(child);
  if (result.code !== 0) throw new Error("runner isolation tests failed");
}

function viteCommand(run) {
  return [
    "--dir",
    "frontend",
    "exec",
    "vite",
    "--host",
    "127.0.0.1",
    "--port",
    String(run.vitePort),
  ];
}

function appCommand(run) {
  return [
    "dev",
    "-m",
    "-nosyncgomod",
    "-skipbindings",
    "-nogorebuild",
    "-frontenddevserverurl",
    run.viteURL,
    "-devserver",
    `127.0.0.1:${run.port}`,
  ];
}

async function main() {
  await runNodeGuards();

  const before = await snapshotDatabaseMetadata(productionAndDevelopmentRoots());
  const run = await createRunContext();
  writeFileSync(run.protectedMetadataBefore, `${JSON.stringify(before, null, 2)}\n`, {
    mode: 0o600,
  });
  const supervisor = new ProcessSupervisor();
  let exitCode = 1;
  let failure;
  let interrupted = false;
  let artifactDir;

  const stopForSignal = async (signal) => {
    interrupted = true;
    failure = new Error(`runner received ${signal}`);
    await supervisor.stopAll();
  };
  const handlers = new Map(
    ["SIGINT", "SIGTERM"].map((signal) => {
      const handler = () => void stopForSignal(signal);
      process.once(signal, handler);
      return [signal, handler];
    }),
  );

  try {
    mkdirSync(join(REPO_ROOT, "frontend", "dist"), { recursive: true });
    writeFileSync(join(REPO_ROOT, "frontend", "dist", ".keep"), "");

    const vite = supervisor.track(
      spawnLogged("pnpm", viteCommand(run), {
        cwd: REPO_ROOT,
        env: process.env,
        logPath: join(run.logsDir, "vite.log"),
      }),
    );
    const viteExit = childResult(vite);
    await Promise.race([
      waitForURL(run.viteURL),
      viteExit.then((result) => {
        throw new Error(`Vite exited before readiness (code ${result.code})`);
      }),
    ]);

    const overlayPath = createAppOverlay(REPO_ROOT, run.runRoot);
    const app = supervisor.track(
      spawnLogged("wails", appCommand(run), {
        cwd: join(REPO_ROOT, "e2e", "app"),
        env: {
          ...appEnvironment(run),
          GOFLAGS: `${process.env.GOFLAGS ?? ""} -overlay=${overlayPath}`.trim(),
        },
        logPath: run.appLog,
      }),
    );
    const appExit = childResult(app);
    await Promise.race([
      waitForURL(run.baseURL),
      appExit.then((result) => {
        throw new Error(`dedicated E2E app exited before readiness (code ${result.code})`);
      }),
    ]);
    if (interrupted) throw failure;

    const playwright = supervisor.track(
      spawn(
        process.execPath,
        [playwrightCli, "test", ...process.argv.slice(2)],
        {
          cwd: here,
          stdio: "inherit",
          env: playwrightEnvironment(run),
          detached: process.platform !== "win32",
        },
      ),
    );
    const result = await childResult(playwright);
    if (result.code !== 0) {
      throw new Error(
        result.signal
          ? `Playwright terminated by ${result.signal}`
          : `Playwright failed with exit ${result.code}`,
      );
    }
    exitCode = 0;
  } catch (error) {
    failure = error;
  } finally {
    await supervisor.stopAll();
    for (const [signal, handler] of handlers) process.removeListener(signal, handler);

    const after = await snapshotDatabaseMetadata(productionAndDevelopmentRoots());
    writeFileSync(run.protectedMetadataAfter, `${JSON.stringify(after, null, 2)}\n`, {
      mode: 0o600,
    });
    const polluted = changedDatabaseMetadata(before, after);
    if (polluted.length > 0) {
      exitCode = 1;
      failure = new Error(
        `storage isolation violation: protected SQLite metadata changed:\n${polluted.join("\n")}`,
      );
    }

    if (exitCode === 0) {
      await run.remove();
    } else {
      artifactDir = await preserveFailureArtifacts(run, artifactRoot);
    }
  }

  if (failure) console.error(failure.message);
  if (artifactDir) console.error(`sanitized E2E artifacts preserved at ${artifactDir}`);
  process.exit(exitCode);
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
