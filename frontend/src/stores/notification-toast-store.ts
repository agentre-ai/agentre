import { create } from "zustand";

import type { NotifyKind } from "../lib/turn-notify";

// NotificationToast 一条应用内提示。title 为会话名（已由通知逻辑按 i18n 取好），
// body 为状态文案，sessionId 供「跳转到会话」与头像查找用。
export type NotificationToast = {
  id: number;
  sessionId: number;
  kind: NotifyKind;
  title: string;
  body: string;
};

// 同时最多保留几条，超出丢弃最旧，避免会话密集完成时无限堆叠。
const MAX_TOASTS = 5;

type State = {
  toasts: NotificationToast[];
  push: (toast: Omit<NotificationToast, "id">) => number;
  dismiss: (id: number) => void;
  clear: () => void;
};

let seq = 0;

export const useNotificationToastStore = create<State>((set) => ({
  toasts: [],
  push: (toast) => {
    const id = ++seq;
    set((s) => {
      const next = [...s.toasts, { ...toast, id }];
      return {
        toasts:
          next.length > MAX_TOASTS
            ? next.slice(next.length - MAX_TOASTS)
            : next,
      };
    });
    return id;
  },
  dismiss: (id) =>
    set((s) => ({ toasts: s.toasts.filter((x) => x.id !== id) })),
  clear: () => set({ toasts: [] }),
}));
