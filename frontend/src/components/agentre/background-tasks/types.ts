export type BackgroundTaskKind = "local_bash";
export type BackgroundTaskStatus = "running" | "completed" | "failed";

export interface BackgroundTask {
  toolUseId: string;
  taskId?: string; // 真实 CLI task_id(不可读串,如 b3875slp0)— 直接展示
  kind: BackgroundTaskKind;
  description: string;
  status: BackgroundTaskStatus;
  startedAt?: number; // epoch ms (containing message createtime) — base for live elapsed
  durationMs?: number; // frozen duration for completed/failed bash tasks
  summary?: string; // completion/exit-code text, dynamic
}
