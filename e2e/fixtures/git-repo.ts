import { execFileSync } from "node:child_process";
import { mkdirSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// seedDirtyGitRepo turns `dir` into a real git repository with one committed file (so HEAD
// exists — `git diff --numstat HEAD`, which internal/pkg/workspacefs/git_changes.go's
// GitChanges runs for the "uncommitted" scope, needs a ref to diff against) plus a set of
// brand-new *untracked* files, so the Git panel's "uncommitted" scope has real rows to render.
//
// This is plain filesystem + `git` CLI work, independent of the Go backend: the caller resolves
// `dir` to the exact path the backend will later treat as the session's cwd
// (internal/pkg/agentruntime/cwd.go's AgentCwd: <AppDataDir>/agents/<agentID>/), and the backend
// tolerates the directory already existing (its own `os.MkdirAll` is a no-op on an existing
// dir). Local `git config user.*` is set per-repo (not --global) so this doesn't depend on the
// CI/dev machine having a global git identity configured.
//
// dir is wiped first so re-running a spec (or a `wails dev` hot-reload restart mid-run) against
// the same e2e temp data dir starts from a clean, deterministic working tree instead of
// accumulating leftover files across runs.
export function seedDirtyGitRepo(dir: string, untrackedFiles: readonly string[]): void {
  rmSync(dir, { recursive: true, force: true });
  mkdirSync(dir, { recursive: true });
  const git = (...args: string[]) =>
    execFileSync("git", args, { cwd: dir, stdio: "ignore" });

  git("init", "-q");
  git("config", "user.email", "e2e@example.com");
  git("config", "user.name", "e2e");
  writeFileSync(join(dir, ".gitkeep"), "seed\n");
  git("add", ".gitkeep");
  git("commit", "-q", "-m", "seed");

  for (const name of untrackedFiles) {
    writeFileSync(join(dir, name), `${name}\n`);
  }
}
