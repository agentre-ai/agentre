import { describe, expect, it } from "vitest";

import {
  avatarFromMeta,
  firstLetter,
  tokenToCssColor,
} from "../session-avatar";

describe("tokenToCssColor", () => {
  it("把 agent token 映射成 css 变量", () => {
    expect(tokenToCssColor("agent-1")).toBe("var(--agent-1)");
    expect(tokenToCssColor("agent-10")).toBe("var(--agent-10)");
  });
  it("空 / 非 agent token → null", () => {
    expect(tokenToCssColor(null)).toBeNull();
    expect(tokenToCssColor(undefined)).toBeNull();
    expect(tokenToCssColor("nope")).toBeNull();
  });
});

describe("firstLetter", () => {
  it("取名字首字符", () => {
    expect(firstLetter("Claude")).toBe("C");
    expect(firstLetter("  Gemini")).toBe("G");
  });
  it("空 / undefined → ?", () => {
    expect(firstLetter("")).toBe("?");
    expect(firstLetter("   ")).toBe("?");
    expect(firstLetter(undefined)).toBe("?");
  });
});

describe("avatarFromMeta", () => {
  it("从 meta 推导首字母 + 颜色", () => {
    expect(
      avatarFromMeta({ agentColor: "agent-2", agentName: "Codex" }),
    ).toEqual({ letter: "C", color: "var(--agent-2)" });
  });
  it("meta 缺失时回落灰底问号", () => {
    expect(avatarFromMeta(null)).toEqual({ letter: "?", color: "#94a3b8" });
    expect(avatarFromMeta({})).toEqual({ letter: "?", color: "#94a3b8" });
  });
});
