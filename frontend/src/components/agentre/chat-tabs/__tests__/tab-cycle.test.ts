import { describe, expect, it } from "vitest";

import { cycleTabId } from "../tab-cycle";

describe("cycleTabId", () => {
  it("returns null for an empty tab list", () => {
    expect(cycleTabId([], null, 1)).toBeNull();
    expect(cycleTabId([], "a", -1)).toBeNull();
  });

  it("stays on the same tab when only one tab is open", () => {
    expect(cycleTabId(["a"], "a", 1)).toBe("a");
    expect(cycleTabId(["a"], "a", -1)).toBe("a");
  });

  it("moves to the next tab going forward", () => {
    expect(cycleTabId(["a", "b", "c"], "a", 1)).toBe("b");
    expect(cycleTabId(["a", "b", "c"], "b", 1)).toBe("c");
  });

  it("wraps forward from the last tab to the first", () => {
    expect(cycleTabId(["a", "b", "c"], "c", 1)).toBe("a");
  });

  it("moves to the previous tab going backward", () => {
    expect(cycleTabId(["a", "b", "c"], "c", -1)).toBe("b");
    expect(cycleTabId(["a", "b", "c"], "b", -1)).toBe("a");
  });

  it("wraps backward from the first tab to the last", () => {
    expect(cycleTabId(["a", "b", "c"], "a", -1)).toBe("c");
  });

  it("falls back to the first tab going forward when the active id is unknown", () => {
    expect(cycleTabId(["a", "b", "c"], null, 1)).toBe("a");
    expect(cycleTabId(["a", "b", "c"], "missing", 1)).toBe("a");
  });

  it("falls back to the last tab going backward when the active id is unknown", () => {
    expect(cycleTabId(["a", "b", "c"], null, -1)).toBe("c");
    expect(cycleTabId(["a", "b", "c"], "missing", -1)).toBe("c");
  });
});
