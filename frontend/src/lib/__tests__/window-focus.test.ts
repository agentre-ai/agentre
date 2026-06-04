import { afterEach, describe, expect, it, vi } from "vitest";
import { isWindowFocused } from "../window-focus";

describe("window-focus", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.resetModules();
  });

  it("blur 后失焦, focus 后恢复", () => {
    window.dispatchEvent(new Event("focus"));
    expect(isWindowFocused()).toBe(true);
    window.dispatchEvent(new Event("blur"));
    expect(isWindowFocused()).toBe(false);
    window.dispatchEvent(new Event("focus"));
    expect(isWindowFocused()).toBe(true);
  });

  it("初值按真实状态计算: 启动即后台(失焦)时不误报为聚焦", async () => {
    vi.resetModules();
    vi.spyOn(document, "hasFocus").mockReturnValue(false);
    const mod = await import("../window-focus");
    expect(mod.isWindowFocused()).toBe(false);
  });
});
