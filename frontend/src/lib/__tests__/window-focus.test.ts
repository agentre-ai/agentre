import { describe, expect, it } from "vitest";
import { isWindowFocused } from "../window-focus";

describe("window-focus", () => {
  it("blur 后失焦, focus 后恢复", () => {
    window.dispatchEvent(new Event("focus"));
    expect(isWindowFocused()).toBe(true);
    window.dispatchEvent(new Event("blur"));
    expect(isWindowFocused()).toBe(false);
    window.dispatchEvent(new Event("focus"));
    expect(isWindowFocused()).toBe(true);
  });
});
