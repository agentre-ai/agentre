import { describe, expect, it } from "vitest";

import { COMMAND_PREFIX, parseMode } from "./mode";

describe("parseMode — query → { mode, payload }", () => {
  it("empty query → default mode, empty payload", () => {
    expect(parseMode("")).toEqual({ mode: "default", payload: "" });
  });

  it("plain text → default mode, payload = query", () => {
    expect(parseMode("hello")).toEqual({ mode: "default", payload: "hello" });
    expect(parseMode("年度报告")).toEqual({
      mode: "default",
      payload: "年度报告",
    });
  });

  it(`leading "${COMMAND_PREFIX}" → command mode, payload stripped`, () => {
    expect(parseMode(">")).toEqual({ mode: "command", payload: "" });
    expect(parseMode(">new")).toEqual({ mode: "command", payload: "new" });
  });

  it("command mode trims leading whitespace after prefix", () => {
    expect(parseMode("> ")).toEqual({ mode: "command", payload: "" });
    expect(parseMode(">   New chat")).toEqual({
      mode: "command",
      payload: "New chat",
    });
  });

  it("preserves trailing whitespace in payload (user is typing)", () => {
    expect(parseMode("> New chat with ")).toEqual({
      mode: "command",
      payload: "New chat with ",
    });
  });

  it(`only the FIRST char is the prefix — "${COMMAND_PREFIX}" inside text stays in default`, () => {
    expect(parseMode("foo>bar")).toEqual({
      mode: "default",
      payload: "foo>bar",
    });
  });

  it("does not crash on unusual but valid input", () => {
    expect(parseMode(">>")).toEqual({ mode: "command", payload: ">" });
  });
});
