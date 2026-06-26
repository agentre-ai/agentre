import { create } from "zustand";
import { LoadChatSession } from "../../wailsjs/go/app/App";
import {
  deriveSubagents,
  type SubagentLite,
} from "../components/agentre/orchestration/subagent-data";

type State = {
  bySession: Map<number, SubagentLite[]>;
  loading: Set<number>;
  ensureLoaded: (sessionId: number) => void;
  __reset: () => void;
};

// orch-subagents-store:按 sessionId 懒加载该 session 的 CLI 子代理。
// 计数需折叠态可见(设计稿 `+N 子代理`),只能 LoadChatSession 数 transcript。
// 读多写零、不碰 orch_svc;同 sessionId 只加载一次(in-flight 去重)。
export const useOrchSubagentsStore = create<State>((set, get) => ({
  bySession: new Map(),
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
        const subs = deriveSubagents(resp?.messages ?? []);
        set((s) => {
          const next = new Map(s.bySession);
          next.set(sessionId, subs);
          const ld = new Set(s.loading);
          ld.delete(sessionId);
          return { bySession: next, loading: ld };
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
  __reset: () => set({ bySession: new Map(), loading: new Set() }),
}));
