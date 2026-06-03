import { describe, expect, it } from "vitest";

import {
  filterByQuery,
  listAvailable,
  slashCommands,
  type SlashExec,
} from "../registry";

describe("slash command registry", () => {
  it("claudecode 可用 /compact", () => {
    const xs = listAvailable("claudecode");
    expect(xs.map((c) => c.name)).toContain("compact");
    const compact = xs.find((c) => c.name === "compact")!;
    const exec = compact.resolve("claudecode")!;
    expect(exec.kind).toBe("literal_text");
    expect((exec as Extract<SlashExec, { kind: "literal_text" }>).text).toBe(
      "/compact",
    );
  });

  it.each(["codex", "piagent"])(
    "%s /compact 也走 literal_text (Enter 时由 chat-panel 拦截转 Compact RPC)",
    (backend) => {
      const xs = listAvailable(backend);
      expect(xs.map((c) => c.name)).toContain("compact");
      const compact = xs.find((c) => c.name === "compact")!;
      const exec = compact.resolve(backend)!;
      expect(exec.kind).toBe("literal_text");
      expect((exec as Extract<SlashExec, { kind: "literal_text" }>).text).toBe(
        "/compact",
      );
    },
  );

  it("codex 可用 /goal，选择后只补全文字，Enter 时由 chat-panel 转 Goal RPC", () => {
    const xs = listAvailable("codex");
    expect(xs.map((c) => c.name)).toContain("goal");
    const goal = xs.find((c) => c.name === "goal")!;
    const exec = goal.resolve("codex")!;
    expect(exec.kind).toBe("literal_text");
    expect((exec as Extract<SlashExec, { kind: "literal_text" }>).text).toBe(
      "/goal ",
    );
  });

  it("空 backend 返回空列表", () => {
    expect(listAvailable("")).toEqual([]);
  });

  it("filterByQuery 大小写不敏感的前缀匹配", () => {
    expect(filterByQuery(slashCommands, "")).toEqual(slashCommands);
    expect(filterByQuery(slashCommands, "comp").map((c) => c.name)).toEqual([
      "compact",
    ]);
    expect(filterByQuery(slashCommands, "COMP").map((c) => c.name)).toEqual([
      "compact",
    ]);
    expect(filterByQuery(slashCommands, "go").map((c) => c.name)).toEqual([
      "goal",
    ]);
    expect(filterByQuery(slashCommands, "xyz")).toEqual([]);
  });
});
