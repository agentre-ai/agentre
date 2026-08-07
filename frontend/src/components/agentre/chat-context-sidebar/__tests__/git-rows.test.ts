import { describe, expect, it } from "vitest";

import { deriveGitRows } from "../git-rows";

import type { workspace_fs_svc } from "@/../wailsjs/go/models";

type Change = workspace_fs_svc.ChangeView;

function change(partial: Partial<Change> & { path: string }): Change {
  return {
    oldPath: "",
    status: "modified",
    added: 0,
    deleted: 0,
    binary: false,
    ...partial,
  } as Change;
}

describe("deriveGitRows", () => {
  it("splits every path into a basename and a grey directory suffix", () => {
    const rows = deriveGitRows([
      change({ path: "internal/service/chat_svc/turn.go" }),
      change({ path: "go.mod" }),
    ]);

    expect(rows.map((r) => [r.name, r.dir])).toEqual([
      ["go.mod", ""],
      ["turn.go", "internal/service/chat_svc"],
    ]);
  });

  it("sorts by the full path so untracked files interleave instead of clumping", () => {
    const rows = deriveGitRows([
      change({ path: "internal/app/workspace_fs.go", status: "added" }),
      change({ path: "docs/spec.md", status: "untracked" }),
      change({ path: "frontend/src/legacy/old-tree.tsx", status: "deleted" }),
      change({ path: "go.mod" }),
    ]);

    expect(rows.map((r) => r.path)).toEqual([
      "docs/spec.md",
      "frontend/src/legacy/old-tree.tsx",
      "go.mod",
      "internal/app/workspace_fs.go",
    ]);
  });

  it("carries the status, diff counts, binary flag and the rename source", () => {
    const rows = deriveGitRows([
      change({
        path: "internal/pkg/workspacefs/git.go",
        oldPath: "internal/pkg/gitfs/git.go",
        status: "renamed",
        added: 42,
        deleted: 7,
      }),
      change({ path: "assets/logo.png", status: "untracked", binary: true }),
    ]);

    expect(rows[1]).toEqual({
      path: "internal/pkg/workspacefs/git.go",
      name: "git.go",
      dir: "internal/pkg/workspacefs",
      oldPath: "internal/pkg/gitfs/git.go",
      status: "renamed",
      added: 42,
      deleted: 7,
      binary: false,
    });
    expect(rows[0].binary).toBe(true);
    expect(rows[0].status).toBe("untracked");
  });

  it("tolerates a missing change list", () => {
    expect(deriveGitRows(null)).toEqual([]);
    expect(deriveGitRows(undefined)).toEqual([]);
  });
});
