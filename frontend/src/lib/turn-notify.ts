import type { TFunction } from "i18next";

import type { AgentStatus } from "../stores/types";
import type { DoneEvent } from "../stores/session-status-store";
import type { NotificationSettings } from "../stores/notification-settings-store";
import type { SoundPreset } from "./notify-sound";

export type NotifyKind = "done" | "error" | "waiting";

export type NotifyDeps = {
  isWindowFocused: () => boolean;
  getActiveSessionId: () => number | null;
  getSettings: () => NotificationSettings;
  getSessionTitle: (sessionId: number) => string | undefined;
  showSystemNotification: (title: string, body: string) => void;
  playSound: (preset: SoundPreset) => void;
  showToast: (kind: NotifyKind, title: string, body: string) => void;
  t: TFunction;
};

// classifyTransition 把一次 agentStatus 转换归类为通知类型；仅在「离开 running」时触发。
// 用户自己点停止（lastDoneEventKind==="aborted"）不通知。
export function classifyTransition(
  prev: AgentStatus | undefined,
  next: AgentStatus,
  lastDoneEventKind: DoneEvent["kind"] | undefined,
): NotifyKind | null {
  if (prev !== "running") return null;
  if (next === "error") return "error";
  if (next === "waiting") return "waiting";
  if (next === "idle") return lastDoneEventKind === "aborted" ? null : "done";
  return null;
}

// maybeNotify 在门槛通过时，按设置触发各启用渠道。
// onlyWhenUnfocused 开（默认）：仅窗口失焦时通知；关：失焦 或 非当前会话 时通知。
export function maybeNotify(
  sessionId: number,
  kind: NotifyKind,
  deps: NotifyDeps,
): void {
  const s = deps.getSettings();
  if (!s.enabled) return;
  const focused = deps.isWindowFocused();
  const suppress = s.onlyWhenUnfocused
    ? focused
    : focused && deps.getActiveSessionId() === sessionId;
  if (suppress) return;

  const title = deps.getSessionTitle(sessionId) ?? deps.t("notify.app");
  const body = deps.t(`notify.body.${kind}`);
  if (s.system) deps.showSystemNotification(title, body);
  if (s.sound) deps.playSound(s.soundPreset);
  if (s.toast) deps.showToast(kind, title, body);
}
