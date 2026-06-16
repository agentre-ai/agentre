import { create } from "zustand";

import { GetAppSetting, UpdateAppSettings } from "../../wailsjs/go/app/App";
import type { app_settings_svc } from "../../wailsjs/go/models";

export type NotificationSettings = {
  enabled: boolean;
  onlyWhenUnfocused: boolean;
  system: boolean;
  toast: boolean;
};

export const DEFAULT_NOTIFICATION_SETTINGS: NotificationSettings = {
  enabled: true,
  onlyWhenUnfocused: true,
  system: true,
  toast: false,
};

const KEYS = {
  enabled: "notify.enabled",
  onlyWhenUnfocused: "notify.only_when_unfocused",
  system: "notify.system",
  toast: "notify.toast",
} as const;

// GetAppSetting 在 key 不存在时会 reject（后端 AppSettingNotFound），逐 key 兜底默认值。
async function readRaw(key: string): Promise<string | null> {
  try {
    const r = await GetAppSetting({ key });
    return r?.value ?? null;
  } catch {
    return null;
  }
}

type State = {
  settings: NotificationSettings;
  load: () => Promise<void>;
  save: (patch: Partial<NotificationSettings>) => Promise<void>;
};

export const useNotificationSettingsStore = create<State>((set, get) => ({
  settings: { ...DEFAULT_NOTIFICATION_SETTINGS },
  load: async () => {
    const [enabled, onlyWhenUnfocused, system, toast] = await Promise.all([
      readRaw(KEYS.enabled),
      readRaw(KEYS.onlyWhenUnfocused),
      readRaw(KEYS.system),
      readRaw(KEYS.toast),
    ]);
    const d = DEFAULT_NOTIFICATION_SETTINGS;
    set({
      settings: {
        enabled: enabled === null ? d.enabled : enabled === "true",
        onlyWhenUnfocused:
          onlyWhenUnfocused === null
            ? d.onlyWhenUnfocused
            : onlyWhenUnfocused === "true",
        system: system === null ? d.system : system === "true",
        toast: toast === null ? d.toast : toast === "true",
      },
    });
  },
  save: async (patch) => {
    const entries = Object.entries(patch).map(([k, v]) => ({
      key: KEYS[k as keyof NotificationSettings],
      value: String(v),
    }));
    if (entries.length === 0) return;
    await UpdateAppSettings({ entries } as app_settings_svc.UpdateRequest);
    set({ settings: { ...get().settings, ...patch } });
  },
}));
