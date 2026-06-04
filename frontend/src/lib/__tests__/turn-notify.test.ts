import { describe, expect, it, vi } from "vitest";
import { DEFAULT_NOTIFICATION_SETTINGS } from "../../stores/notification-settings-store";
import {
  classifyTransition,
  maybeNotify,
  type NotifyDeps,
} from "../turn-notify";

describe("classifyTransition", () => {
  it("running→idle = done", () => {
    expect(classifyTransition("running", "idle", "done")).toBe("done");
  });
  it("running→idle 但 aborted = null(用户自己停的)", () => {
    expect(classifyTransition("running", "idle", "aborted")).toBeNull();
  });
  it("running→error = error", () => {
    expect(classifyTransition("running", "error", "error")).toBe("error");
  });
  it("running→waiting = waiting", () => {
    expect(classifyTransition("running", "waiting", undefined)).toBe("waiting");
  });
  it("非 running 起点不触发", () => {
    expect(classifyTransition("idle", "running", undefined)).toBeNull();
    expect(classifyTransition(undefined, "running", undefined)).toBeNull();
  });
});

function deps(over: Partial<NotifyDeps> = {}): NotifyDeps {
  return {
    isWindowFocused: () => false,
    getActiveSessionId: () => 0,
    getSettings: () => ({
      ...DEFAULT_NOTIFICATION_SETTINGS,
      toast: true,
    }),
    getSessionTitle: () => "我的会话",
    showSystemNotification: vi.fn(),
    showToast: vi.fn(),
    t: ((k: string) => k) as NotifyDeps["t"],
    ...over,
  };
}

describe("maybeNotify", () => {
  it("默认(仅失焦)+ 失焦 → 触发全部已启用渠道", () => {
    const d = deps();
    maybeNotify(42, "done", d);
    expect(d.showSystemNotification).toHaveBeenCalledWith(
      42,
      "我的会话",
      "notify.body.done",
    );
    expect(d.showToast).toHaveBeenCalledWith(
      42,
      "done",
      "我的会话",
      "notify.body.done",
    );
  });

  it("默认(仅失焦)+ 聚焦(任意会话)→ 全部静默", () => {
    const d = deps({
      isWindowFocused: () => true,
      getActiveSessionId: () => 7,
    });
    maybeNotify(42, "done", d);
    expect(d.showSystemNotification).not.toHaveBeenCalled();
    expect(d.showToast).not.toHaveBeenCalled();
  });

  it("关掉 onlyWhenUnfocused + 聚焦 + 非当前会话 → 触发", () => {
    const d = deps({
      isWindowFocused: () => true,
      getActiveSessionId: () => 7,
      getSettings: () => ({
        ...DEFAULT_NOTIFICATION_SETTINGS,
        onlyWhenUnfocused: false,
        toast: true,
      }),
    });
    maybeNotify(42, "done", d);
    expect(d.showSystemNotification).toHaveBeenCalled();
  });

  it("关掉 onlyWhenUnfocused + 聚焦 + 当前会话 → 静默", () => {
    const d = deps({
      isWindowFocused: () => true,
      getActiveSessionId: () => 42,
      getSettings: () => ({
        ...DEFAULT_NOTIFICATION_SETTINGS,
        onlyWhenUnfocused: false,
        toast: true,
      }),
    });
    maybeNotify(42, "done", d);
    expect(d.showSystemNotification).not.toHaveBeenCalled();
  });

  it("总开关关 → 不触发", () => {
    const d = deps({
      getSettings: () => ({
        ...DEFAULT_NOTIFICATION_SETTINGS,
        enabled: false,
      }),
    });
    maybeNotify(42, "done", d);
    expect(d.showSystemNotification).not.toHaveBeenCalled();
  });

  it("无 session 标题时回落 notify.app", () => {
    const d = deps({ getSessionTitle: () => undefined });
    maybeNotify(42, "error", d);
    expect(d.showSystemNotification).toHaveBeenCalledWith(
      42,
      "notify.app",
      "notify.body.error",
    );
  });

  it("只开系统通知时不弹 toast", () => {
    const d = deps({
      getSettings: () => ({
        ...DEFAULT_NOTIFICATION_SETTINGS,
        toast: false,
        system: true,
      }),
    });
    maybeNotify(42, "done", d);
    expect(d.showSystemNotification).toHaveBeenCalled();
    expect(d.showToast).not.toHaveBeenCalled();
  });
});
