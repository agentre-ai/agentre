import { describe, expect, it } from "vitest";

import { deriveFiles, deriveOutline, type FileEntry } from "../derive";

import type { chat_svc } from "../../../../../wailsjs/go/models";

type Msg = chat_svc.ChatMessage;

function userMsg(id: number, text: string, t = 0): Msg {
  return {
    id, role: "user", sessionId: 1, blocks: [{ type: "text", text }],
    model: "", promptTokens: 0, completionTokens: 0, durationMs: 0,
    errorText: "", seq: 0, createtime: t,
  } as unknown as Msg;
}

function assistantWithEdits(id: number, files: string[], errored = false): Msg {
  const blocks = files.map((p) => ({
    type: "tool_use", name: "Edit", input: { file_path: p },
  }));
  return {
    id, role: "assistant", sessionId: 1, blocks,
    model: "", promptTokens: 0, completionTokens: 0, durationMs: 0,
    errorText: errored ? "boom" : "", seq: 0, createtime: 0,
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
});

describe("deriveFiles", () => {
  it("aggregates Edit/Write/MultiEdit by file_path across turns", () => {
    const msgs = [
      userMsg(1, "u1"), assistantWithEdits(2, ["a.go", "a.go", "b.go"]),
      userMsg(3, "u2"), assistantWithEdits(4, ["a.go"]),
    ];
    const files = deriveFiles(msgs);
    const a = files.find((f: FileEntry) => f.path === "a.go")!;
    const b = files.find((f: FileEntry) => f.path === "b.go")!;
    expect(a.edits).toBe(3);
    expect(b.edits).toBe(1);
    expect(a.lastTurn).toBe(2);
    expect(b.lastTurn).toBe(1);
  });

  it("sorts files by edits desc, ties broken by recency (lastTurn desc)", () => {
    const msgs = [
      userMsg(1, "u1"), assistantWithEdits(2, ["a.go"]),
      userMsg(3, "u2"), assistantWithEdits(4, ["b.go"]),
    ];
    const files = deriveFiles(msgs);
    expect(files[0].path).toBe("b.go"); // tie on edits, b.go more recent
  });

  it("returns empty array for empty input", () => {
    expect(deriveFiles([])).toEqual([]);
  });
});
