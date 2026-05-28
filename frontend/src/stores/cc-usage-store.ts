// cc-usage-store
//
// 单例 zustand store,索引 Claude Code OAuth usage state 按 deviceKey:
//   - "local"        → 桌面所在机器的 5h / 7d 配额
//   - "remote:<id>"  → 某台已配对 agentred 上的配额
//
// 数据源:后端 cc_usage_svc 在 wails event "cc_usage:update" 推送;
// hook use-cc-usage 订阅一次,把 payload 灌到 byDevice。
//
// 同值短路:byDevice Map 引用只在真有数据变动时更新,避免 60s tick
// 把数字 round 成同一个百分比却让所有订阅者集体 re-render。

import { create } from "zustand";

import type { cc_usage_svc } from "../../wailsjs/go/models";

export type UsageState = cc_usage_svc.UsageState;

type State = {
  byDevice: Map<string, UsageState>;
};

type Actions = {
  upsert: (deviceKey: string, state: UsageState) => void;
  remove: (deviceKey: string) => void;
  __reset: () => void;
};

// shallowSameState 判断两个 UsageState 是否在 HUD 显示维度上相同。
// 比较 reason / stale / fetchedAtMs 以及 data 的 5h、7d、sonnet/opus 百分比。
// resets_at 字段是 ISO 字符串,直接 === 比较即可。
function shallowSameState(a: UsageState, b: UsageState): boolean {
  if (a.reason !== b.reason) return false;
  if ((a.stale ?? false) !== (b.stale ?? false)) return false;
  if (a.fetchedAtMs !== b.fetchedAtMs) return false;
  const da = a.data;
  const db = b.data;
  if (!da && !db) return true;
  if (!da || !db) return false;
  return (
    da.fiveHourPercent === db.fiveHourPercent &&
    da.weeklyPercent === db.weeklyPercent &&
    (da.sonnetWeeklyPercent ?? null) === (db.sonnetWeeklyPercent ?? null) &&
    (da.opusWeeklyPercent ?? null) === (db.opusWeeklyPercent ?? null) &&
    (da.fiveHourResetsAt ?? null) === (db.fiveHourResetsAt ?? null) &&
    (da.weeklyResetsAt ?? null) === (db.weeklyResetsAt ?? null)
  );
}

export const useCCUsageStore = create<State & Actions>((set) => ({
  byDevice: new Map(),

  upsert: (deviceKey, state) =>
    set((s) => {
      const prev = s.byDevice.get(deviceKey);
      if (prev && shallowSameState(prev, state)) return s;
      const byDevice = new Map(s.byDevice);
      byDevice.set(deviceKey, state);
      return { byDevice };
    }),

  remove: (deviceKey) =>
    set((s) => {
      if (!s.byDevice.has(deviceKey)) return s;
      const byDevice = new Map(s.byDevice);
      byDevice.delete(deviceKey);
      return { byDevice };
    }),

  __reset: () => set({ byDevice: new Map() }),
}));

// useCCUsageFor 给单个 deviceKey 返回当前 state,未拉过 → undefined。
export function useCCUsageFor(deviceKey: string): UsageState | undefined {
  return useCCUsageStore((s) => s.byDevice.get(deviceKey));
}
