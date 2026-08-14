import assert from "node:assert/strict";
import {
  existsSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  rmSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { extname, join, relative, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { test } from "node:test";

import { GUARD_TESTS } from "./guard-suite.mjs";
import { REPO_ROOT } from "./run-context.mjs";

const read = (path) => readFileSync(join(REPO_ROOT, path), "utf8");

function localModuleSpecifiers(source) {
  const specifiers = new Set();
  const patterns = [
    /(?:^|\n)\s*import\s+(?:[^"';()]*?\s+from\s+)?["']([^"']+)["']/gs,
    /(?:^|\n)\s*export\s+[^"';()]*?\s+from\s+["']([^"']+)["']/gs,
    /\bimport\s*\(\s*["']([^"']+)["']\s*\)/g,
  ];
  for (const pattern of patterns) {
    for (const match of source.matchAll(pattern)) {
      if (match[1].startsWith(".")) specifiers.add(match[1]);
    }
  }
  return [...specifiers];
}

function collectLocalModuleGraph(entryPath) {
  const pending = [resolve(entryPath)];
  const graph = new Map();

  while (pending.length > 0) {
    const modulePath = pending.pop();
    if (graph.has(modulePath)) continue;
    assert.equal(existsSync(modulePath), true, `local ESM module is missing: ${modulePath}`);
    assert.equal(statSync(modulePath).isFile(), true, `local ESM import is not a file: ${modulePath}`);

    const source = readFileSync(modulePath, "utf8");
    graph.set(modulePath, source);
    for (const specifier of localModuleSpecifiers(source)) {
      const moduleURL = new URL(specifier, pathToFileURL(modulePath));
      assert.equal(moduleURL.protocol, "file:", `${modulePath} has a non-file local import ${specifier}`);
      const dependencyPath = fileURLToPath(moduleURL);
      assert.notEqual(
        extname(dependencyPath),
        "",
        `${modulePath} local ESM import must include its extension: ${specifier}`,
      );
      assert.equal(
        existsSync(dependencyPath),
        true,
        `${modulePath} imports missing local ESM module ${specifier}`,
      );
      assert.equal(
        statSync(dependencyPath).isFile(),
        true,
        `${modulePath} local ESM import is not a file: ${specifier}`,
      );
      if (!graph.has(dependencyPath)) pending.push(dependencyPath);
    }
  }

  return graph;
}

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

test("Given the automated E2E runner entry, when its complete local ESM graph is inspected, then no module can discover installed or development data roots", () => {
  const entryPath = join(REPO_ROOT, "e2e", "run-e2e.mjs");
  const graph = collectLocalModuleGraph(entryPath);
  assert.equal(graph.has(entryPath), true);
  assert.equal(graph.has(join(REPO_ROOT, "e2e", "lib", "run-context.mjs")), true);

  const forbiddenCapabilities = [
    {
      name: "OS account-home discovery",
      pattern: /\b(?:homedir|userInfo)\b/,
    },
    {
      name: "production-root environment lookup",
      pattern: /\b(?:HOME|USERPROFILE|APPDATA|LOCALAPPDATA|XDG_CONFIG_HOME)\b/,
    },
    {
      name: "macOS installed/development data-root literal",
      pattern:
        /(?:Library[\\/]Application Support[\\/]agentre(?:-dev)?|["']Library["']\s*,\s*["']Application Support["']\s*,\s*["']agentre(?:-dev)?["'])/,
    },
    {
      name: "Linux installed/development data-root literal",
      pattern: /(?:\.config[\\/]agentre(?:-dev)?|["']\.config["']\s*,\s*["']agentre(?:-dev)?["'])/,
    },
    {
      name: "Windows installed/development data-root literal",
      pattern:
        /(?:AppData[\\/](?:Roaming|Local)[\\/]agentre(?:-dev)?|["']AppData["']\s*,\s*["'](?:Roaming|Local)["']\s*,\s*["']agentre(?:-dev)?["'])/i,
    },
    {
      name: "development data-root leaf literal",
      pattern: /["']agentre-dev["']/,
    },
  ];

  for (const [modulePath, source] of graph) {
    for (const capability of forbiddenCapabilities) {
      assert.doesNotMatch(
        source,
        capability.pattern,
        `${relative(REPO_ROOT, modulePath)} has forbidden ${capability.name}`,
      );
    }
  }
});

test("Given local ESM imports, when the runner graph is collected, then extension-bearing dependencies are followed and missing modules fail loudly", (t) => {
  const fixtureRoot = mkdtempSync(join(tmpdir(), "agentre-runner-graph-"));
  t.after(() => rmSync(fixtureRoot, { recursive: true, force: true }));
  const dependencyPath = join(fixtureRoot, "dependency.mjs");
  const entryPath = join(fixtureRoot, "entry.mjs");
  writeFileSync(dependencyPath, "export const dependency = true;\n");
  writeFileSync(entryPath, 'import { dependency } from "./dependency.mjs";\nvoid dependency;\n');

  const graph = collectLocalModuleGraph(entryPath);
  assert.equal(graph.has(dependencyPath), true);

  writeFileSync(entryPath, 'import "./missing.mjs";\n');
  assert.throws(
    () => collectLocalModuleGraph(entryPath),
    /imports missing local ESM module \.\/missing\.mjs/,
  );
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
