// group-list-store 是「左侧 sidebar 群聊分区列表」的唯一数据源。形态和
// chat-agents-store 一致:全局 store + reload 并发去重,让 sidebar / 其它消费方
// 共享同一份群列表,任何一处 reload 都立刻被所有订阅者看到。
//
// MVP:只负责 mount 时拉一次 + 暴露 reload。live 刷新(建群 / run_status 变更)
// 由调用方在需要时显式 reload(),本任务范围内不订阅 group:event:*。

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
  __reset: () => {
    inflight = null;
    set({ groups: [], loading: true, error: null });
  },
}));
