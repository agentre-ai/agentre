import { create } from "zustand";
import { LoadChatSession } from "../../wailsjs/go/app/App";
import {
  deriveSubagents,
  type SubagentLite,
} from "../components/agentre/orchestration/subagent-data";
import type { chat_svc } from "../../wailsjs/go/models";

type State = {
  bySession: Map<number, SubagentLite[]>;
  messagesBySession: Map<number, chat_svc.ChatMessage[]>;
  loading: Set<number>;
  ensureLoaded: (sessionId: number) => void;
  messagesFor: (sessionId: number) => chat_svc.ChatMessage[];
  __reset: () => void;
};

// orch-subagents-store:按 sessionId 懒加载该 session 的 CLI 子代理。
// 计数需折叠态可见(设计稿 `+N 子代理`),只能 LoadChatSession 数 transcript。
// 读多写零、不碰 orch_svc;同 sessionId 只加载一次(in-flight 去重)。
export const useOrchSubagentsStore = create<State>((set, get) => ({
  bySession: new Map(),
  messagesBySession: new Map(),
  loading: new Set(),
  ensureLoaded: (sessionId) => {
    if (!sessionId) return;
    const { bySession, loading } = get();
    if (bySession.has(sessionId) || loading.has(sessionId)) return;
    set((s) => {
      const ld = new Set(s.loading);
      ld.add(sessionId);
      return { loading: ld };
    });
    void LoadChatSession({ sessionId } as never)
      .then((resp) => {
        const messages = resp?.messages ?? [];
        const subs = deriveSubagents(messages);
        set((s) => {
          const nextSubs = new Map(s.bySession);
          nextSubs.set(sessionId, subs);
          const nextMsgs = new Map(s.messagesBySession);
          nextMsgs.set(sessionId, messages);
          const ld = new Set(s.loading);
          ld.delete(sessionId);
          return {
            bySession: nextSubs,
            messagesBySession: nextMsgs,
            loading: ld,
          };
        });
      })
      .catch(() => {
        set((s) => {
          const ld = new Set(s.loading);
          ld.delete(sessionId);
          return { loading: ld };
        });
      });
  },
  messagesFor: (sessionId) => get().messagesBySession.get(sessionId) ?? [],
  __reset: () =>
    set({ bySession: new Map(), messagesBySession: new Map(), loading: new Set() }),
}));
