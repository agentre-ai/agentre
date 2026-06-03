import { act, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const showNotification = vi.fn((_req: unknown) => Promise.resolve());
// 仅 sound/toast 命中 → "true",其余 reject(走默认值)。让 load() 产出
// enabled/onlyWhenUnfocused/system 默认开 + sound/toast 开,且不依赖时序。
vi.mock("../../../../wailsjs/go/app/App", () => ({
  ShowNotification: (req: unknown) => showNotification(req),
  GetAppSetting: (req: { key: string }) =>
    req.key === "notify.sound" || req.key === "notify.toast"
      ? Promise.resolve({ key: req.key, value: "true" })
      : Promise.reject(new Error("nf")),
  UpdateAppSettings: vi.fn(() => Promise.resolve({})),
}));
const playSound = vi.fn();
vi.mock("../../../lib/notify-sound", () => ({
  playNotifySound: (p: unknown) => playSound(p),
  SOUND_PRESETS: ["ding", "chime", "blip"],
}));
const toastSuccess = vi.fn();
vi.mock("sonner", () => ({
  toast: {
    success: (...a: unknown[]) => toastSuccess(...a),
    error: vi.fn(),
    warning: vi.fn(),
  },
}));
let focused = false;
vi.mock("../../../lib/window-focus", () => ({
  isWindowFocused: () => focused,
}));

import { useSessionStatusStore } from "../../../stores/session-status-store";
import { useChatTabsStore } from "../../../stores/chat-tabs-store";
import {
  DEFAULT_NOTIFICATION_SETTINGS,
  useNotificationSettingsStore,
} from "../../../stores/notification-settings-store";
import { TurnCompleteNotifier } from "../turn-complete-notifier";

beforeEach(() => {
  vi.clearAllMocks();
  focused = false;
  useSessionStatusStore.getState().__reset();
  useChatTabsStore.setState({ tabs: [], activeTabId: null });
  useNotificationSettingsStore.setState({
    settings: { ...DEFAULT_NOTIFICATION_SETTINGS, sound: true, toast: true },
  });
});
afterEach(() => vi.restoreAllMocks());

describe("TurnCompleteNotifier", () => {
  it("非当前会话 running→idle 触发系统通知+声音+toast", async () => {
    render(<TurnCompleteNotifier />);
    await act(async () => {}); // 让 load() 完成
    act(() => {
      useSessionStatusStore
        .getState()
        .upsert(42, { agentStatus: "running", needsAttention: false });
    });
    act(() => {
      useSessionStatusStore
        .getState()
        .upsert(42, { agentStatus: "idle", needsAttention: false });
      useSessionStatusStore.getState().bumpDone(42, { kind: "done" });
    });
    expect(showNotification).toHaveBeenCalledTimes(1);
    expect(playSound).toHaveBeenCalledWith("ding");
    expect(toastSuccess).toHaveBeenCalledTimes(1);
  });

  it("挂载前已存在的 idle 会话不误报", async () => {
    act(() => {
      useSessionStatusStore
        .getState()
        .upsert(7, { agentStatus: "idle", needsAttention: false });
    });
    render(<TurnCompleteNotifier />);
    await act(async () => {});
    expect(showNotification).not.toHaveBeenCalled();
  });
});
