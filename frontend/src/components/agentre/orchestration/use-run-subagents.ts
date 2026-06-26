import * as React from "react";
import type { app } from "../../../../wailsjs/go/models";
import { useOrchSubagentsStore } from "../../../stores/orch-subagents-store";
import type { SubagentLite } from "./subagent-data";

// useRunSubagents:对 run 的每个 task session 触发懒加载,给任务板/结构图共享读。
export function useRunSubagents(detail: app.RunDetailDTO): {
  forSession: (sessionId: number) => SubagentLite[];
  countForAgent: (agentId: number) => number;
} {
  const tasks = React.useMemo(() => detail.tasks ?? [], [detail.tasks]);
  const bySession = useOrchSubagentsStore((s) => s.bySession);
  const ensureLoaded = useOrchSubagentsStore((s) => s.ensureLoaded);

  React.useEffect(() => {
    for (const t of tasks) {
      if (t.sessionId) ensureLoaded(t.sessionId);
    }
  }, [tasks, ensureLoaded]);

  return React.useMemo(() => {
    const forSession = (sessionId: number) => bySession.get(sessionId) ?? [];
    const countForAgent = (agentId: number) =>
      tasks
        .filter((t) => t.agentId === agentId)
        .reduce((n, t) => n + (bySession.get(t.sessionId)?.length ?? 0), 0);
    return { forSession, countForAgent };
  }, [bySession, tasks]);
}
