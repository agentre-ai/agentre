import { describe, expect, it } from "vitest";
import { Capabilities } from "../types";

describe("Capabilities skills/mcp_tools membership", () => {
  it("recognizes the skills and mcp_tools capability strings", () => {
    const caps = new Capabilities(new Set(["skills", "mcp_tools"]), {
      allowedModes: [],
      defaultMode: "",
      switchableDuringTurn: false,
      order: [],
    });
    expect(caps.has("skills")).toBe(true);
    expect(caps.has("mcp_tools")).toBe(true);
  });
});
