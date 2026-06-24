import { create } from "zustand";
import { RunLoad } from "../../wailsjs/go/app/App";
import type { app } from "../../wailsjs/go/models";
import { ORCH_EVENTS } from "../components/agentre/orchestration/events";

interface OrchRunState {
  details: Map<number, app.RunDetailDTO>;
  deadlocks: Map<number, number[]>;
  loadRun: (id: number) => Promise<void>;
  onRunEvent: (
    name: string,
    payload: { runId: number; cycle?: number[] },
  ) => void;
  __reset: () => void;
}

export const useOrchRunStore = create<OrchRunState>((set, get) => ({
  details: new Map(),
  deadlocks: new Map(),
  async loadRun(id) {
    const d = await RunLoad(id);
    const m = new Map(get().details);
    m.set(id, d);
    set({ details: m });
  },
  onRunEvent(name, payload) {
    if (!payload?.runId) return;
    if (name === ORCH_EVENTS.deadlock && payload.cycle) {
      const dm = new Map(get().deadlocks);
      dm.set(payload.runId, payload.cycle);
      set({ deadlocks: dm });
    }
    // 任何 run 事件 → 重新拉详情(简单可靠;数据量小)。
    void get().loadRun(payload.runId);
  },
  __reset: () => set({ details: new Map(), deadlocks: new Map() }),
}));
