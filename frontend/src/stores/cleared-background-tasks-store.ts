import { create } from "zustand";
import { persist } from "zustand/middleware";

type ClearedBackgroundTasksState = {
  // sessionId -> 已清理(dismiss)的 toolUseId 列表
  cleared: Record<number, string[]>;
  clearCompleted: (sessionId: number, toolUseIds: string[]) => void;
};

export const useClearedBackgroundTasksStore =
  create<ClearedBackgroundTasksState>()(
    persist(
      (set) => ({
        cleared: {},
        clearCompleted: (sessionId, toolUseIds) => {
          if (toolUseIds.length === 0) return;
          set((state) => {
            const prev = state.cleared[sessionId] ?? [];
            const merged = [...new Set([...prev, ...toolUseIds])];
            return { cleared: { ...state.cleared, [sessionId]: merged } };
          });
        },
      }),
      { name: "cleared-background-tasks" },
    ),
  );
