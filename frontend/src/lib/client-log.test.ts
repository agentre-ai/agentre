import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// 用 hoisted mock 拦住 Wails 生成的 binding —— headless 测试里 window.go 不存在。
const logClientMock = vi.hoisted(() => vi.fn());
vi.mock("../../wailsjs/go/app/App", () => ({
  LogClient: logClientMock,
}));

import { clientLog } from "./client-log";

describe("clientLog", () => {
  beforeEach(() => {
    logClientMock.mockReset();
    logClientMock.mockResolvedValue(undefined);
    vi.spyOn(console, "warn").mockImplementation(() => {});
    vi.spyOn(console, "info").mockImplementation(() => {});
    vi.spyOn(console, "error").mockImplementation(() => {});
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("when warn called, then forwards level/scope/message/fields to the backend bridge", () => {
    clientLog.warn("use-chat-session", "stale overwrite", { sessionId: 79 });

    expect(logClientMock).toHaveBeenCalledTimes(1);
    expect(logClientMock).toHaveBeenCalledWith({
      level: "warn",
      scope: "use-chat-session",
      message: "stale overwrite",
      fields: { sessionId: 79 },
    });
  });

  it("when warn called, then also writes to DevTools console (dev visibility kept)", () => {
    clientLog.warn("scope", "msg", { a: 1 });

    expect(console.warn).toHaveBeenCalledWith("[scope] msg", { a: 1 });
  });

  it("when info/error called, then maps to the matching backend level", () => {
    clientLog.info("scope-a", "m1");
    clientLog.error("scope-b", "m2", { x: 1 });

    expect(logClientMock).toHaveBeenNthCalledWith(1, {
      level: "info",
      scope: "scope-a",
      message: "m1",
      fields: undefined,
    });
    expect(logClientMock).toHaveBeenNthCalledWith(2, {
      level: "error",
      scope: "scope-b",
      message: "m2",
      fields: { x: 1 },
    });
  });

  it("when the backend bridge rejects, then it does not throw (fire-and-forget)", async () => {
    logClientMock.mockRejectedValue(new Error("binding down"));

    expect(() => clientLog.warn("scope", "msg")).not.toThrow();
    // 让被拒绝的 promise 落地,确认不会冒泡成 unhandled rejection。
    await Promise.resolve();
  });

  it("when the binding throws synchronously, then it does not throw", () => {
    logClientMock.mockImplementation(() => {
      throw new Error("binding not injected");
    });

    expect(() => clientLog.warn("scope", "msg")).not.toThrow();
  });
});
