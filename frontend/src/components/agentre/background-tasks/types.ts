export type BackgroundTaskKind = "local_bash" | "local_agent";
export type BackgroundTaskStatus = "running" | "completed" | "failed";

export interface BackgroundTask {
  toolUseId: string;
  kind: BackgroundTaskKind;
  description: string;
  status: BackgroundTaskStatus;
  startedAt?: number; // epoch ms (containing message createtime) — base for live elapsed
  durationMs?: number; // frozen duration for completed/failed subagents (from subagent.durationMs)
  summary?: string; // completion/exit-code text (from subagent.summary), dynamic
}
