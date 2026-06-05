export type BackgroundTaskKind = "local_bash" | "local_agent";
export type BackgroundTaskStatus = "running" | "completed" | "failed";

export interface BackgroundTask {
  toolUseId: string;
  kind: BackgroundTaskKind;
  description: string;
  status: BackgroundTaskStatus;
}
