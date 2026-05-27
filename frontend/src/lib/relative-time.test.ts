import { describe, expect, it } from "vitest";
import { relativeTime } from "./relative-time";

const NOW = new Date("2026-05-16T12:00:00Z").getTime();

describe("relativeTime", () => {
  it("returns 'now' for < 60s", () => {
    expect(relativeTime(NOW - 30_000, NOW)).toBe("now");
  });
  it("returns Nm for minutes", () => {
    expect(relativeTime(NOW - 3 * 60_000, NOW)).toBe("3m");
  });
  it("returns Nh for hours", () => {
    expect(relativeTime(NOW - 2 * 3600_000, NOW)).toBe("2h");
  });
  it("returns Nd for days", () => {
    expect(relativeTime(NOW - 4 * 86400_000, NOW)).toBe("4d");
  });
  it("returns '' for zero/negative", () => {
    expect(relativeTime(0, NOW)).toBe("");
  });
});
