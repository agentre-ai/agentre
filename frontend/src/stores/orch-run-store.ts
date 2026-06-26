import { create } from "zustand";
import { RunLoad } from "../../wailsjs/go/app/App";
import type { app } from "../../wailsjs/go/models";
import { ORCH_EVENTS } from "../components/agentre/orchestration/events";

export interface AskLogItem {
  kind: "ask" | "reply";
  askId: string;
  agentId: number;
  targetAgentId?: number;
  text: string;
  ts: number;
}

export interface ActiveAsk {
  askId: string;
  askerAgentId: number;
  targetAgentId: number;
}

interface OrchRunState {
  details: Map<number, app.RunDetailDTO>;
  deadlocks: Map<number, number[]>;
  askLog: Map<number, AskLogItem[]>;
  activeAsks: Map<number, ActiveAsk[]>;
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
  askLog: new Map(),
  activeAsks: new Map(),
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
    if (name === ORCH_EVENTS.ask) {
      const p = payload as unknown as {
        runId: number;
        askId: string;
        askerAgentId: number;
        targetAgentId: number;
        question: string;
      };
      const log = new Map(get().askLog);
      const arr = [
        ...(log.get(p.runId) ?? []),
        {
          kind: "ask" as const,
          askId: p.askId,
          agentId: p.askerAgentId,
          targetAgentId: p.targetAgentId,
          text: p.question,
          ts: Date.now(),
        },
      ];
      log.set(p.runId, arr);
      const act = new Map(get().activeAsks);
      act.set(p.runId, [
        ...(act.get(p.runId) ?? []),
        {
          askId: p.askId,
          askerAgentId: p.askerAgentId,
          targetAgentId: p.targetAgentId,
        },
      ]);
      set({ askLog: log, activeAsks: act });
      return;
    }
    if (name === ORCH_EVENTS.reply) {
      const p = payload as unknown as {
        runId: number;
        askId: string;
        answer: string;
        timedOut: boolean;
      };
      const prevAsk = (get().activeAsks.get(p.runId) ?? []).find(
        (a) => a.askId === p.askId,
      );
      const act = new Map(get().activeAsks);
      act.set(
        p.runId,
        (act.get(p.runId) ?? []).filter((a) => a.askId !== p.askId),
      );
      const log = new Map(get().askLog);
      log.set(p.runId, [
        ...(log.get(p.runId) ?? []),
        {
          kind: "reply" as const,
          askId: p.askId,
          agentId: prevAsk?.targetAgentId ?? 0,
          text: p.timedOut ? "" : p.answer,
          ts: Date.now(),
        },
      ]);
      set({ askLog: log, activeAsks: act });
      return;
    }
    // 任何 run 事件 → 重新拉详情(简单可靠;数据量小)。
    void get().loadRun(payload.runId);
  },
  __reset: () =>
    set({
      details: new Map(),
      deadlocks: new Map(),
      askLog: new Map(),
      activeAsks: new Map(),
    }),
}));
