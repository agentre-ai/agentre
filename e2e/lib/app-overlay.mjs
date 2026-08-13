import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const FAILURE_INJECTION_POINT = "\t\t// 超长思考流接缝(e2e-long-thinking:<runes>):先流式发一大段 thinking,再发短文本";
const FAILURE_BRANCH = `\t\tif reason, found := parseOnePartDirective(req.UserText, "e2e-runtime-fail:"); found {
\t\t\tselect {
\t\t\tcase <-ctx.Done():
\t\t\t\treturn
\t\t\tcase out <- agentruntime.ErrorEvent{Err: fmt.Errorf("e2e-runtime-failure: %s", reason)}:
\t\t\t}
\t\t\treturn
\t\t}
`;

export function createAppOverlay(repoRoot, runRoot) {
  const fakeRuntime = join(
    repoRoot,
    "internal",
    "pkg",
    "agentruntime",
    "runtimes",
    "fake",
    "runtime.go",
  );
  const source = readFileSync(fakeRuntime, "utf8");
  if (!source.includes(FAILURE_INJECTION_POINT)) {
    throw new Error("fake runtime changed: deterministic failure overlay cannot be applied safely");
  }
  const runtimeOverlay = join(runRoot, "fake-runtime.go");
  writeFileSync(
    runtimeOverlay,
    source.replace(FAILURE_INJECTION_POINT, FAILURE_BRANCH + FAILURE_INJECTION_POINT),
    { mode: 0o600 },
  );

  const overlayPath = join(runRoot, "go-overlay.json");
  writeFileSync(
    overlayPath,
    `${JSON.stringify({
      Replace: {
        [fakeRuntime]: runtimeOverlay,
      },
    })}\n`,
    { mode: 0o600 },
  );
  return overlayPath;
}
