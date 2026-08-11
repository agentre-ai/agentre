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

// DroppedQueue 记录「回合收尾时还没被 AI 消费、原本会静默丢弃」的排队条目。
// sessionId 记录来源会话，restoreDropped 按它把条目放回原队列。
export type DroppedQueue = {
  sessionId: number;
  items: QueuedMessage[];
  at: number;
} | null;

type State = {
  queuedBySession: Map<number, QueuedMessage[]>;
  // 最近一次被「标记为丢弃」的排队条目（同一时刻最多一条）。null = 无。
  dropped: DroppedQueue;
};

type Actions = {
  append: (sessionId: number, msg: QueuedMessage) => void;
  // consume 移除指定 ids（不传则取出全部并清空）。返回被移除的条目（供 doCancelQueued 使用）。
  consume: (sessionId: number, ids?: string[]) => QueuedMessage[];
  // clear 清空指定 session 的所有排队条目（finishStream 路径）。
  clear: (sessionId: number) => void;
  // markDropped 把指定 session 的排队条目整体挪进 dropped 并清空队列（回合收尾
  // 未消费路径）。队列为空时 no-op，不清任何东西，也不覆盖已有 dropped。
  markDropped: (sessionId: number) => void;
  // dismissDropped 丢弃 dropped 记录（用户选择「丢弃」）。
  dismissDropped: () => void;
  // restoreDropped 把 dropped 条目按原 session 追加回排队队列（用户选择「恢复为草稿」）。
  restoreDropped: () => void;
  // 测试隔离用，生产代码不该调。
  __reset: () => void;
};

export const useQueuedMessagesStore = create<State & Actions>((set, get) => ({
  queuedBySession: new Map(),
  dropped: null,

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

  markDropped: (sessionId) =>
    set((state) => {
      const items = state.queuedBySession.get(sessionId);
      if (!items || items.length === 0) return state;
      const next = new Map(state.queuedBySession);
      next.delete(sessionId);
      return {
        queuedBySession: next,
        dropped: { sessionId, items, at: Date.now() },
      };
    }),

  dismissDropped: () => set({ dropped: null }),

  restoreDropped: () =>
    set((state) => {
      if (!state.dropped) return state;
      const { sessionId, items } = state.dropped;
      const cur = state.queuedBySession.get(sessionId) ?? [];
      const next = new Map(state.queuedBySession);
      next.set(sessionId, [...cur, ...items]);
      return { queuedBySession: next, dropped: null };
    }),

  __reset: () => set({ queuedBySession: new Map(), dropped: null }),
}));
