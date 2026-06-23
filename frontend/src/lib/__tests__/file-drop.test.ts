import { beforeEach, describe, expect, it, vi } from "vitest";

import { OnFileDrop, OnFileDropOff } from "../../../wailsjs/runtime/runtime";
import { __resetDropRegistryForTest, registerDropZone } from "../file-drop";

vi.mock("../../../wailsjs/runtime/runtime", () => ({
  OnFileDrop: vi.fn(),
  OnFileDropOff: vi.fn(),
}));

// 取最近一次传给 OnFileDrop 的回调。
function dropCallback(): (x: number, y: number, paths: string[]) => void {
  const calls = (OnFileDrop as unknown as ReturnType<typeof vi.fn>).mock.calls;
  return calls[calls.length - 1][0];
}

describe("file-drop 路由", () => {
  beforeEach(() => {
    __resetDropRegistryForTest();
    vi.mocked(OnFileDrop).mockClear();
    vi.mocked(OnFileDropOff).mockClear();
  });

  it("首个注册装上全局 OnFileDrop", () => {
    const el = document.createElement("div");
    registerDropZone(el, () => {});
    expect(OnFileDrop).toHaveBeenCalledTimes(1);
    expect(OnFileDrop).toHaveBeenCalledWith(expect.any(Function), true);
  });

  it("drop 路由到光标命中的 zone", () => {
    const a = document.createElement("div");
    const b = document.createElement("div");
    const hitA = vi.fn();
    const hitB = vi.fn();
    registerDropZone(a, hitA);
    registerDropZone(b, hitB);

    document.elementFromPoint = vi.fn(() => b) as never;
    dropCallback()(10, 20, ["/a/x.txt"]);

    expect(hitB).toHaveBeenCalledWith(["/a/x.txt"]);
    expect(hitA).not.toHaveBeenCalled();
  });

  it("光标不在任何 zone → 不抛、不调用", () => {
    const a = document.createElement("div");
    const hitA = vi.fn();
    registerDropZone(a, hitA);
    document.elementFromPoint = vi.fn(() => document.body) as never;
    expect(() => dropCallback()(1, 1, ["/a/x.txt"])).not.toThrow();
    expect(hitA).not.toHaveBeenCalled();
  });

  it("最后一个注销时卸掉监听", () => {
    const el = document.createElement("div");
    const off = registerDropZone(el, () => {});
    off();
    expect(OnFileDropOff).toHaveBeenCalledTimes(1);
  });
});
