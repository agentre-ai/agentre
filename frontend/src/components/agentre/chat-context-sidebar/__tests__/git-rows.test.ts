import { describe, expect, it } from "vitest";

import { countSubtreeChanges, deriveGitRows } from "../git-rows";

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

describe("countSubtreeChanges", () => {
  it("counts only paths nested under the directory, not the directory's own path or a sibling with a shared prefix", () => {
    const paths = [
      "internal/service/chat_svc/turn.go",
      "internal/service/chat_svc/cwd.go",
      "internal/app/system.go",
      "internal/service", // 目录自身恰好出现在变动清单里(理论不该发生,但不应计入自己的子树)
      "internal/service-legacy/old.go", // 前缀相同但不是子目录,不该被当成子树的一部分
      "go.mod",
    ];

    expect(countSubtreeChanges(paths, "internal/service/chat_svc")).toBe(2);
    expect(countSubtreeChanges(paths, "internal/service")).toBe(2);
    // "internal" 前缀下还多两条：不属于 chat_svc 子树的 internal/app/system.go，
    // 以及本身就以 "internal/" 开头的 "internal/service" 与
    // "internal/service-legacy/old.go"（后两条各自被上面两个断言排除在外，说明
    // 排除逻辑是针对更深前缀的，不是全局黑名单）。
    expect(countSubtreeChanges(paths, "internal")).toBe(5);
  });

  it("treats an empty directory path as the tree root, counting the entire change list", () => {
    const paths = ["a.go", "dir/b.go", "dir/sub/c.go"];
    expect(countSubtreeChanges(paths, "")).toBe(3);
  });

  it("returns zero for a directory with no changes in its subtree", () => {
    expect(countSubtreeChanges(["internal/a.go"], "assets")).toBe(0);
    expect(countSubtreeChanges([], "internal")).toBe(0);
  });

  it("gives a compacted chain's frontier the same count as the whole chain, since a chain's intermediate segments never hold files directly", () => {
    // internal -> service -> chat_svc 被压缩成一行时,链尾(chat_svc)本身就是唯一
    // 持有文件的目录,对链尾调用一次即得到整条链的统计,调用方不需要特殊处理。
    const paths = [
      "internal/service/chat_svc/turn.go",
      "internal/service/chat_svc/cwd.go",
    ];
    expect(countSubtreeChanges(paths, "internal/service/chat_svc")).toBe(2);
  });
});
