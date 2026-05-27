export type TaskStatus =
  | "queued"
  | "running"
  | "completed"
  | "cancelled"
  | "failed";

export type Task = {
  id: string;
  description: string;
  status: TaskStatus;
};

// TaskProgress 现在就是一个 Task[];runtime 来源(Claude / Codex)由 backend 在
// canonical.PlanUpdate 里收编完毕,前端不再分支 source。
export type TaskProgress = {
  tasks: Task[];
};
