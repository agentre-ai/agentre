// frontend/src/stores/queued-messages-store.ts
//
// queued-messages-store 是「当前 turn 进行中排队消息」的独立 store。
// 与 chat-streams-store 解耦，职责单一：持有 append / consume / clear 操作。
//
// 消费方：
//   - chat-panel.tsx: 读 queuedBySession.get(sid) 渲染 QueuedMessagesBar；
//     doEnqueue 调 append；doCancelQueued 调 consume（按 id 过滤）。
//   - chat-streams-store.finishStream: 调 clear 清空该 session 排队。
//   - chat-streams-store.consumeSteer: 调 consume（按 ids 过滤）消费掉被后端取走的条目。

import { create } from "zustand";

// QueuedMessage 字段与 QueuedItem（queued-messages-bar.tsx）对齐。
export type QueuedMessage = {
  id: string;
  text: string;
  cancellable: boolean;
};

type State = {
  queuedBySession: Map<number, QueuedMessage[]>;
};

type Actions = {
  append: (sessionId: number, msg: QueuedMessage) => void;
  // consume 移除指定 ids（不传则取出全部并清空）。返回被移除的条目（供 doCancelQueued 使用）。
  consume: (sessionId: number, ids?: string[]) => QueuedMessage[];
  // clear 清空指定 session 的所有排队条目（finishStream 路径）。
  clear: (sessionId: number) => void;
  // 测试隔离用，生产代码不该调。
  __reset: () => void;
};

export const useQueuedMessagesStore = create<State & Actions>((set, get) => ({
  queuedBySession: new Map(),

  append: (sessionId, msg) =>
    set((state) => {
      const cur = state.queuedBySession.get(sessionId) ?? [];
      const next = new Map(state.queuedBySession);
      next.set(sessionId, [...cur, msg]);
      return { queuedBySession: next };
    }),

  consume: (sessionId, ids) => {
    const all = get().queuedBySession.get(sessionId) ?? [];
    if (ids === undefined) {
      // 全部取出并清空
      set((state) => {
        if (!state.queuedBySession.has(sessionId)) return state;
        const next = new Map(state.queuedBySession);
        next.delete(sessionId);
        return { queuedBySession: next };
      });
      return all;
    }
    const idSet = new Set(ids);
    const removed = all.filter((m) => idSet.has(m.id));
    set((state) => {
      if (!state.queuedBySession.has(sessionId)) return state;
      const remaining = (state.queuedBySession.get(sessionId) ?? []).filter(
        (m) => !idSet.has(m.id),
      );
      const next = new Map(state.queuedBySession);
      if (remaining.length === 0) next.delete(sessionId);
      else next.set(sessionId, remaining);
      return { queuedBySession: next };
    });
    return removed;
  },

  clear: (sessionId) =>
    set((state) => {
      if (!state.queuedBySession.has(sessionId)) return state;
      const next = new Map(state.queuedBySession);
      next.delete(sessionId);
      return { queuedBySession: next };
    }),

  __reset: () => set({ queuedBySession: new Map() }),
}));
