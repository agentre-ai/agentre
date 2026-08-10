import { describe, expect, it } from "vitest";

import {
  collapseDirChain,
  deriveFileTree,
  deriveFiles,
  deriveOutline,
  type FileEntry,
} from "../derive";

import type { chat_svc } from "../../../../../wailsjs/go/models";

type Msg = chat_svc.ChatMessage;

function userMsg(id: number, text: string, t = 0): Msg {
  return {
    id,
    role: "user",
    sessionId: 1,
    blocks: [{ type: "text", text }],
    model: "",
    promptTokens: 0,
    completionTokens: 0,
    durationMs: 0,
    errorText: "",
    seq: 0,
    createtime: t,
  } as unknown as Msg;
}

function assistantWithEdits(id: number, files: string[], errored = false): Msg {
  const blocks = files.map((p) => ({
    type: "tool_use",
    toolName: "Edit",
    toolInput: { file_path: p },
  }));
  return {
    id,
    role: "assistant",
    sessionId: 1,
    blocks,
    model: "",
    promptTokens: 0,
    completionTokens: 0,
    durationMs: 0,
    errorText: errored ? "boom" : "",
    seq: 0,
    createtime: 0,
  } as unknown as Msg;
}

function editBlock(
  toolName: string,
  file_path: string,
  canonicalFiles: Array<{ path: string; plus?: number; minus?: number }>,
) {
  return {
    type: "tool_use",
    toolName,
    toolInput: { file_path },
    canonical: {
      kind: "file.edit",
      fileEdit: {
        files: canonicalFiles.map((f) => ({
          kind: "patch",
          hunks: [],
          path: f.path,
          plus: f.plus,
          minus: f.minus,
        })),
      },
    },
  };
}

function writeBlock(toolName: string, path: string, lines: number) {
  return {
    type: "tool_use",
    toolName,
    toolInput: { file_path: path },
    canonical: {
      kind: "file.write",
      fileWrite: { path, content: "", lines, bytes: 0 },
    },
  };
}

function assistantWithBlocks(id: number, blocks: unknown[]): Msg {
  return {
    id,
    role: "assistant",
    sessionId: 1,
    blocks,
    model: "",
    promptTokens: 0,
    completionTokens: 0,
    durationMs: 0,
    errorText: "",
    seq: 0,
    createtime: 0,
  } as unknown as Msg;
}

describe("deriveOutline", () => {
  it("treats each user message as one row in chronological order", () => {
    const msgs = [userMsg(1, "first", 1000), userMsg(2, "second", 2000)];
    const out = deriveOutline(msgs);
    expect(out).toHaveLength(2);
    expect(out[0].turn).toBe(1);
    expect(out[1].turn).toBe(2);
    expect(out[0].text).toBe("first");
  });

  it("counts edits between this user msg and the next", () => {
    const msgs = [
      userMsg(1, "do edits"),
      assistantWithEdits(2, ["a.go", "b.go"]),
      userMsg(3, "next"),
      assistantWithEdits(4, ["c.go"]),
    ];
    const out = deriveOutline(msgs);
    expect(out[0].edits).toBe(2);
    expect(out[1].edits).toBe(1);
  });

  it("marks err=true if the following assistant has errorText", () => {
    const msgs = [userMsg(1, "trigger"), assistantWithEdits(2, [], true)];
    const out = deriveOutline(msgs);
    expect(out[0].err).toBe(true);
  });

  it("returns empty array for empty input", () => {
    expect(deriveOutline([])).toEqual([]);
  });

  it("renders @ mention XML in the message text as readable @label, not raw tags", () => {
    const msgs = [userMsg(1, 'ping <agent id="1">CEO 助手</agent> now')];
    const out = deriveOutline(msgs);
    expect(out[0].text).toBe("ping @CEO 助手 now");
  });
});

describe("deriveFiles", () => {
  it("sums plus/minus from file.edit canonical across hunks and files", () => {
    const msgs = [
      userMsg(1, "u1"),
      assistantWithBlocks(2, [
        editBlock("Edit", "a.go", [
          { path: "a.go", plus: 5, minus: 2 },
          { path: "a.go", plus: 3, minus: 0 },
          { path: "b.go", plus: 1, minus: 1 },
        ]),
      ]),
    ];
    const files = deriveFiles(msgs);
    const a = files.find((f: FileEntry) => f.path === "a.go")!;
    const b = files.find((f: FileEntry) => f.path === "b.go")!;
    expect(a.plus).toBe(8);
    expect(a.minus).toBe(2);
    expect(a.lastTurn).toBe(1);
    expect(b.plus).toBe(1);
    expect(b.minus).toBe(1);
  });

  it("counts file.write canonical lines as plus for that path", () => {
    const msgs = [
      userMsg(1, "u1"),
      assistantWithBlocks(2, [writeBlock("Write", "new.go", 42)]),
    ];
    const files = deriveFiles(msgs);
    const f = files.find((x: FileEntry) => x.path === "new.go")!;
    expect(f.plus).toBe(42);
    expect(f.minus).toBe(0);
    expect(f.lastTurn).toBe(1);
  });

  it("keeps legacy blocks without canonical but with 0 plus/minus", () => {
    const msgs = [
      userMsg(1, "u1"),
      assistantWithBlocks(2, [
        {
          type: "tool_use",
          toolName: "Edit",
          toolInput: { file_path: "a.go" },
        },
      ]),
    ];
    const files = deriveFiles(msgs);
    const a = files.find((f: FileEntry) => f.path === "a.go")!;
    expect(a.plus).toBe(0);
    expect(a.minus).toBe(0);
    expect(a.lastTurn).toBe(1);
  });

  it("ignores empty raw and canonical paths so malformed tool blocks cannot create an openable project-directory row", () => {
    const msgs = [
      userMsg(1, "u1"),
      assistantWithBlocks(2, [
        {
          type: "tool_use",
          toolName: "Edit",
          toolInput: { file_path: "" },
          canonical: {
            kind: "file.edit",
            fileEdit: {
              files: [
                {
                  kind: "patch",
                  hunks: [],
                  path: "",
                  plus: 1,
                  minus: 0,
                },
              ],
            },
          },
        },
      ]),
    ];

    expect(deriveFiles(msgs)).toEqual([]);
  });

  it("keeps read-tool paths in the list with 0 plus/minus", () => {
    const msgs = [
      userMsg(1, "u1"),
      assistantWithBlocks(2, [
        { type: "tool_use", toolName: "read", toolInput: { path: "a.go" } },
      ]),
    ];
    const files = deriveFiles(msgs);
    const a = files.find((f: FileEntry) => f.path === "a.go")!;
    expect(a.plus).toBe(0);
    expect(a.minus).toBe(0);
    expect(a.lastTurn).toBe(1);
  });

  it("sorts by plus+minus desc, ties broken by lastTurn desc", () => {
    const msgs = [
      userMsg(1, "u1"),
      assistantWithBlocks(2, [
        editBlock("Edit", "a.go", [{ path: "a.go", plus: 1, minus: 1 }]),
      ]),
      userMsg(3, "u2"),
      assistantWithBlocks(4, [
        editBlock("Edit", "b.go", [{ path: "b.go", plus: 3, minus: 0 }]),
      ]),
      userMsg(5, "u3"),
      assistantWithBlocks(6, [
        editBlock("Edit", "c.go", [{ path: "c.go", plus: 2, minus: 0 }]),
      ]),
    ];
    const files = deriveFiles(msgs);
    expect(files.map((f) => f.path)).toEqual(["b.go", "c.go", "a.go"]);
  });

  it("Given Claude mutation blocks, When files are derived, Then Edit, Write, and MultiEdit paths are listed", () => {
    const toolNames = ["Edit", "Write", "MultiEdit"];
    const msgs = [
      userMsg(1, "u1"),
      {
        id: 2,
        role: "assistant",
        sessionId: 1,
        blocks: toolNames.map((toolName, index) => ({
          type: "tool_use",
          toolName,
          toolInput: { file_path: `${index}.go` },
        })),
        model: "",
        promptTokens: 0,
        completionTokens: 0,
        durationMs: 0,
        errorText: "",
        seq: 0,
        createtime: 0,
      } as unknown as Msg,
    ];

    expect(deriveFiles(msgs).map((f) => f.path)).toEqual([
      "0.go",
      "1.go",
      "2.go",
    ]);
  });

  it("Given a Codex file_change block, When files are derived, Then every changed path is listed", () => {
    const msgs = [
      userMsg(1, "u1"),
      {
        id: 2,
        role: "assistant",
        sessionId: 1,
        blocks: [
          {
            type: "tool_use",
            toolName: "file_change",
            toolInput: {
              changes: [{ path: "a.go" }, { path: "b.go" }],
            },
          },
        ],
        model: "",
        promptTokens: 0,
        completionTokens: 0,
        durationMs: 0,
        errorText: "",
        seq: 0,
        createtime: 0,
      } as unknown as Msg,
    ];

    expect(deriveFiles(msgs).map((f) => f.path)).toEqual(["a.go", "b.go"]);
  });

  it("returns empty array for empty input", () => {
    expect(deriveFiles([])).toEqual([]);
  });
});

describe("deriveFileTree", () => {
  const e = (path: string): FileEntry => ({
    path,
    plus: 0,
    minus: 0,
    lastTurn: 1,
  });

  it("builds nested dir/file hierarchy, dirs before files, files in input order", () => {
    const tree = deriveFileTree([e("a/b/c.go"), e("a/d.go"), e("e.go")]);
    expect(tree).toEqual([
      {
        kind: "dir",
        name: "a",
        children: [
          {
            kind: "dir",
            name: "b",
            children: [
              {
                kind: "file",
                entry: { path: "a/b/c.go", plus: 0, minus: 0, lastTurn: 1 },
              },
            ],
          },
          {
            kind: "file",
            entry: { path: "a/d.go", plus: 0, minus: 0, lastTurn: 1 },
          },
        ],
      },
      { kind: "file", entry: { path: "e.go", plus: 0, minus: 0, lastTurn: 1 } },
    ]);
  });

  it("sorts dir nodes alphabetically, keeps file input order", () => {
    const files = [
      e("x/b.go"),
      e("a/c.go"),
      e("m/f.go"),
      e("a/z.go"),
      e("a/a.go"),
    ];
    const tree = deriveFileTree(files);
    expect(tree.filter((n) => n.kind === "dir").map((n) => n.name)).toEqual([
      "a",
      "m",
      "x",
    ]);
    const a = tree.find((n) => n.kind === "dir" && n.name === "a")!;
    expect(a.kind).toBe("dir");
    if (a.kind === "dir") {
      expect(
        a.children.map((n) => (n.kind === "file" ? n.entry.path : n.name)),
      ).toEqual(["a/c.go", "a/z.go", "a/a.go"]);
    }
  });

  it("splits Windows path separators into collapsible directories", () => {
    expect(deriveFileTree([e("src\\components\\file.tsx")])).toEqual([
      {
        kind: "dir",
        name: "src",
        children: [
          {
            kind: "dir",
            name: "components",
            children: [
              {
                kind: "file",
                entry: {
                  path: "src\\components\\file.tsx",
                  plus: 0,
                  minus: 0,
                  lastTurn: 1,
                },
              },
            ],
          },
        ],
      },
    ]);
  });

  it("keeps root files as root-level file nodes", () => {
    expect(deriveFileTree([e("only.go")])).toEqual([
      {
        kind: "file",
        entry: { path: "only.go", plus: 0, minus: 0, lastTurn: 1 },
      },
    ]);
  });

  it("handles deep paths with nested dirs at every level", () => {
    const tree = deriveFileTree([e("a/b/c/d/f.go")]);
    expect(tree[0]).toEqual({
      kind: "dir",
      name: "a",
      children: [
        {
          kind: "dir",
          name: "b",
          children: [
            {
              kind: "dir",
              name: "c",
              children: [
                {
                  kind: "dir",
                  name: "d",
                  children: [
                    {
                      kind: "file",
                      entry: {
                        path: "a/b/c/d/f.go",
                        plus: 0,
                        minus: 0,
                        lastTurn: 1,
                      },
                    },
                  ],
                },
              ],
            },
          ],
        },
      ],
    });
  });

  it("returns empty array for empty input", () => {
    expect(deriveFileTree([])).toEqual([]);
  });
});

describe("collapseDirChain", () => {
  // 「变动」模式的数据形状：整棵树一次性可得，cursor 就是节点本身。
  type TreeChild = { name: string; isDir: boolean; children?: TreeChild[] };
  function treeChildrenOf(node: TreeChild) {
    if (!node.isDir || node.children === undefined) return null;
    return node.children.map((c) => ({
      name: c.name,
      isDir: c.isDir,
      cursor: c,
    }));
  }

  // 「目录」模式的数据形状：逐层懒加载，cursor 是相对路径字符串，未加载的层
  // 在 levels 里没有对应 key（用 undefined 表示「还不知道」，而不是空数组）。
  function levelsChildrenOf(
    levels: Record<string, Array<{ name: string; isDir: boolean }>>,
  ) {
    return (path: string) => {
      const entries = levels[path];
      if (entries === undefined) return null;
      return entries.map((e) => ({
        name: e.name,
        isDir: e.isDir,
        cursor: path ? `${path}/${e.name}` : e.name,
      }));
    };
  }

  it("folds a single-subdirectory, file-free chain into one row (tree data shape)", () => {
    const chatSvc: TreeChild = {
      name: "chat_svc",
      isDir: true,
      children: [{ name: "chat.go", isDir: false }],
    };
    const service: TreeChild = {
      name: "service",
      isDir: true,
      children: [chatSvc],
    };
    const internal: TreeChild = {
      name: "internal",
      isDir: true,
      children: [service],
    };

    const result = collapseDirChain("internal", internal, treeChildrenOf);

    expect(result.names).toEqual(["internal", "service", "chat_svc"]);
    expect(result.children).toEqual([
      {
        name: "chat.go",
        isDir: false,
        cursor: { name: "chat.go", isDir: false },
      },
    ]);
  });

  it("does not fold past a directory that directly contains a file", () => {
    const b: TreeChild = {
      name: "b",
      isDir: true,
      children: [{ name: "x.go", isDir: false }],
    };
    const a: TreeChild = {
      name: "a",
      isDir: true,
      children: [b, { name: "y.go", isDir: false }],
    };

    const result = collapseDirChain("a", a, treeChildrenOf);

    expect(result.names).toEqual(["a"]);
    expect(result.children).toHaveLength(2);
  });

  it("does not fold past a directory with multiple subdirectories", () => {
    const a: TreeChild = {
      name: "a",
      isDir: true,
      children: [
        { name: "b", isDir: true, children: [] },
        { name: "c", isDir: true, children: [] },
      ],
    };

    const result = collapseDirChain("a", a, treeChildrenOf);

    expect(result.names).toEqual(["a"]);
    expect(result.children?.map((c) => c.name)).toEqual(["b", "c"]);
  });

  it("folds a chain that starts at the tree root, not just nested ones", () => {
    // 根层的单子目录链（root 本身没有实体行，链从根下第一个目录开始）与嵌套
    // 处的链走同一条规则——不需要「祖先层已经是链」这个前提。
    const empty: TreeChild = { name: "b", isDir: true, children: [] };
    const a: TreeChild = { name: "a", isDir: true, children: [empty] };

    const result = collapseDirChain("a", a, treeChildrenOf);

    expect(result.names).toEqual(["a", "b"]);
    expect(result.children).toEqual([]);
  });

  it("lazily-loaded shape: folds only the portion already known, without needing an unopened level", () => {
    const levels = {
      internal: [{ name: "service", isDir: true }],
      // "internal/service" 尚未加载：levelsChildrenOf 对它返回 null。
    };
    const childrenOf = levelsChildrenOf(levels);

    const result = collapseDirChain("internal", "internal", childrenOf);

    expect(result.names).toEqual(["internal", "service"]);
    expect(result.cursor).toBe("internal/service");
    expect(result.children).toBeNull();
  });

  it("lazily-loaded shape: extends correctly once a deeper level has arrived", () => {
    const levels = {
      internal: [{ name: "service", isDir: true }],
      "internal/service": [{ name: "chat_svc", isDir: true }],
      "internal/service/chat_svc": [{ name: "chat.go", isDir: false }],
    };
    const childrenOf = levelsChildrenOf(levels);

    const result = collapseDirChain("internal", "internal", childrenOf);

    expect(result.names).toEqual(["internal", "service", "chat_svc"]);
    expect(result.cursor).toBe("internal/service/chat_svc");
    expect(result.children).toEqual([
      {
        name: "chat.go",
        isDir: false,
        cursor: "internal/service/chat_svc/chat.go",
      },
    ]);
  });

  it("lazily-loaded shape: a start whose own level is unloaded folds to itself alone", () => {
    const childrenOf = levelsChildrenOf({});

    const result = collapseDirChain("internal", "internal", childrenOf);

    expect(result.names).toEqual(["internal"]);
    expect(result.cursor).toBe("internal");
    expect(result.children).toBeNull();
  });
});
