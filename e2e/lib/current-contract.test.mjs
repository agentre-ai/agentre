import assert from "node:assert/strict";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";

import { GUARD_TESTS } from "./guard-suite.mjs";
import { REPO_ROOT } from "./run-context.mjs";

const read = (path) => readFileSync(join(REPO_ROOT, path), "utf8");

const legacyPaths = [
  "e2e/playwright.scratch.config.ts",
  "e2e/playwright.sync.config.ts",
  "e2e/run-e2e-sync.mjs",
  "e2e/scratch/README.md",
  "e2e/sync",
  "e2e/fixtures/git-repo.ts",
  "e2e/fixtures/sync.ts",
  "e2e/fakes/remote.go",
];

const currentGuidesAndEntries = [
  "AGENTS.md",
  "Makefile",
  "docs/architecture.md",
  "docs/develop.md",
  "docs/testing.md",
  "docs/verification.md",
  "docs/documentation.md",
  "docs/references/verification-report-template.md",
  "e2e/README.md",
  "e2e/package.json",
  "e2e/verify.mjs",
  "e2e/drive.mjs",
];

const legacyBuildTagDocs = [
  "internal/bootstrap/keychain.go",
  "internal/bootstrap/keychain_test.go",
];

const forbiddenCurrentFacts = [
  "--flavor",
  "FLAVOR=",
  "AGENTRE_VERIFY_FLAVOR",
  "AGENTRE_E2E_REUSE",
  "e2e-scratch",
  "e2e-sync",
  "test:scratch",
  "test:sync",
  "-tags e2e",
  "WindowHide",
];

test("Given the unified harness, when current entries and guides are inspected, then no legacy harness contract remains", () => {
  for (const path of legacyPaths) {
    assert.equal(existsSync(join(REPO_ROOT, path)), false, `${path} must be deleted`);
  }
  for (const path of currentGuidesAndEntries) {
    const source = read(path);
    for (const fact of forbiddenCurrentFacts) {
      assert.equal(source.includes(fact), false, `${path} still contains legacy fact ${fact}`);
    }
  }
  for (const path of legacyBuildTagDocs) {
    assert.equal(read(path).includes("-tags e2e"), false, `${path} still documents the deleted E2E build tag`);
  }
});

test("Given the isolated E2E harness, when runner storage contracts are inspected, then production database metadata is never accessed", () => {
  const inspectedPaths = [
    "e2e/run-e2e.mjs",
    "e2e/lib/run-context.mjs",
    "e2e/README.md",
  ];
  const forbiddenMetadataContracts = [
    "productionAndDevelopmentRoots",
    "snapshotDatabaseMetadata",
    "changedDatabaseMetadata",
    "protected-db-",
    "snapshots installed/development database metadata",
    "protected database metadata",
    "The runner snapshots",
  ];

  for (const path of inspectedPaths) {
    const source = read(path);
    for (const contract of forbiddenMetadataContracts) {
      assert.equal(
        source.includes(contract),
        false,
        `${path} still contains protected metadata contract ${contract}`,
      );
    }
  }
});

test("Given the replacement suite, when its public entries are inspected, then only unified automation and real verification remain", () => {
  assert.deepEqual(
    readdirSync(join(REPO_ROOT, "e2e", "tests")).filter((name) => name.endsWith(".spec.ts")).sort(),
    ["desktop.spec.ts", "remote-peer.spec.ts", "sync-client.spec.ts"],
  );

  const makefile = read("Makefile");
  assert.match(makefile, /^BACKEND_PKGS := .*\.\/e2e\/\.\.\.(?: |$)/m);
  for (const target of ["e2e:", "verify-up:", "verify-status:", "verify-down:"]) {
    assert.match(makefile, new RegExp(`^${target.replace(":", "\\:")}`, "m"));
  }
  assert.match(makefile, /^e2e:\n\tcd e2e && pnpm test$/m);

  const pkg = JSON.parse(read("e2e/package.json"));
  assert.deepEqual(Object.keys(pkg.scripts).sort(), ["drive", "setup", "test", "test:guards", "verify"]);
  assert.equal(pkg.scripts.test, "node run-e2e.mjs");
  assert.equal(pkg.scripts["test:guards"], "node run-guards.mjs");

  const runner = read("e2e/run-e2e.mjs");
  assert.match(runner, /runNodeGuards/);
  assert.equal(runner.includes('"lib\/run-context.test.mjs"'), false);
  assert.deepEqual([...GUARD_TESTS].sort(), [
    "lib/app-overlay.test.mjs",
    "lib/current-contract.test.mjs",
    "lib/fake-sync-server.test.mjs",
    "lib/run-context.test.mjs",
    "lib/target.test.mjs",
  ]);

  const verify = read("e2e/verify.mjs");
  assert.match(verify, /spawn\(wailsBin\(\), wailsArgs, \{[\s\S]*cwd: repoRoot,/);
  assert.equal(verify.includes("e2e/app"), false);
  assert.equal(verify.includes("AGENTRE_E2E_MANIFEST"), false);

  const workflow = read(".github/workflows/ci.yml");
  const e2eJob = workflow.slice(workflow.indexOf("  e2e:"));
  assert.equal((e2eJob.match(/make e2e/g) ?? []).length, 1);
  assert.match(e2eJob, /name: e2e-artifacts-\$\{\{ github\.run_id \}\}-\$\{\{ github\.run_attempt \}\}/);
  assert.match(e2eJob, /e2e\/artifacts\/\*\//);
});
