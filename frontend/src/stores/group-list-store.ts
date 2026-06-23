// group-list-store 是「左侧 sidebar 群聊分区列表」的唯一数据源。形态和
// chat-agents-store 一致:全局 store + reload 并发去重,让 sidebar / 其它消费方
// 共享同一份群列表,任何一处 reload 都立刻被所有订阅者看到。
//
// 结构性变更(建群/删群)仍由调用方显式 reload();run_status 运行态走
// GroupEventsHost 订阅的全局 groups:run_state 事件 → patchRunStatus 增量更新,
// 侧栏群行状态点不开群页也实时翻转。这里不订阅 group:event:*(per-group 频道)。

import { create } from "zustand";

import { GroupList } from "../../wailsjs/go/app/App";
import type { app } from "../../wailsjs/go/models";

export type GroupListItem = app.GroupItem;

type State = {
  groups: GroupListItem[];
  loading: boolean;
  error: string | null;
};

type Actions = {
  reload: () => Promise<void>;
  // patchRunStatus 只改目标群的 runStatus(GroupEventsHost 的 run_status 事件驱动);
  // 未知 id / 同值时返回原 state,订阅者不动。
  patchRunStatus: (groupId: number, runStatus: string) => void;
  // 测试隔离用,生产代码不该调。
  __reset: () => void;
};

// in-flight reload promise:并发调用 reload() 时复用,避免重复 RPC。
let inflight: Promise<void> | null = null;

// 初始 loading=true:反映「还没拉过,别把空 groups 当成最终态」,与 chat-agents-store
// 对齐,避免首帧闪一下空分区。
export const useGroupListStore = create<State & Actions>((set) => ({
  groups: [],
  loading: true,
  error: null,
  reload: () => {
    if (inflight) return inflight;
    set({ loading: true, error: null });
    inflight = (async () => {
      try {
        const groups = (await GroupList()) ?? [];
        set({ groups, loading: false, error: null });
      } catch (e: unknown) {
        const msg = e instanceof Error ? e.message : String(e);
        set({ loading: false, error: msg });
      } finally {
        inflight = null;
      }
    })();
    return inflight;
  },
  patchRunStatus: (groupId, runStatus) =>
    set((state) => {
      const idx = state.groups.findIndex((g) => g.id === groupId);
      if (idx < 0 || state.groups[idx].runStatus === runStatus) return state;
      const groups = state.groups.slice();
      groups[idx] = { ...groups[idx], runStatus } as GroupListItem;
      return { groups };
    }),
  __reset: () => {
    inflight = null;
    set({ groups: [], loading: true, error: null });
  },
}));
