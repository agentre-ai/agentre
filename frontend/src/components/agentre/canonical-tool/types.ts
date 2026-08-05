// MIRROR OF: internal/service/chat_svc/view/chat_block.go.
// Any change here must be reflected on the Go side + round-trip test.

export type CanonicalKind =
  | "file.write"
  | "file.edit"
  | "user.ask"
  | "plan.update"
  | "plan.approve_request"
  | "agent.spawn"
  | "tool.permission";

export type FileWriteDTO = {
  path: string;
  content: string;
  lines: number;
  bytes: number;
  truncated?: boolean;
};

export type DiffOp = " " | "+" | "-";
export type DiffLine = { op: DiffOp; old?: number; new?: number; text: string };
export type DiffHunk = {
  oldStart: number;
  oldLines: number;
  newStart: number;
  newLines: number;
  header?: string;
  lines: DiffLine[];
};
export type FileChangeKind = "created" | "modified" | "deleted";
export type FileEditPatch = {
  path: string;
  kind: FileChangeKind;
  hunks: DiffHunk[];
  plus: number;
  minus: number;
  truncated?: boolean;
  replaceAll?: boolean;
};
export type FileEditDTO = { files: FileEditPatch[] };

export type AskQuestionDTO = {
  id?: string;
  question: string;
  header: string;
  multiSelect?: boolean;
  isOther?: boolean;
  isSecret?: boolean;
  options: { label: string; description: string; preview?: string }[];
};
export type AskAnswerDTO = {
  questionIndex: number;
  labels: string[];
  otherText?: string;
};
export type UserAskDTO = {
  requestId: string;
  questions: AskQuestionDTO[];
  answers?: AskAnswerDTO[];
  answered?: boolean;
  skipped?: boolean;
  expired?: boolean;
};

export type PlanStepDTO = {
  id?: string;
  step: string;
  status: "pending" | "inProgress" | "completed" | "canceled";
};

// PlanActionKind 与 internal/pkg/agentruntime/canonical/plan_action.go 一一对应。
// 前端按 kind 选 button variant/icon。
export type PlanActionKind = "approve" | "refine" | "reject";

// PlanActionDTO 单个按钮的稳定描述,id 是后端装配的 provider-neutral
// plan.* 命名空间 key;前端按 id + kind 渲染,不再分支 backendType/source。
export type PlanActionDTO = {
  id: string;
  kind: PlanActionKind;
  requiresFeedback?: boolean;
};

export type PlanUpdateDTO = {
  steps: PlanStepDTO[];
  text?: string;
  actions?: PlanActionDTO[];
};

export type PlanApproveRequestDTO = {
  requestId: string;
  planText: string;
  resolved?: boolean;
  allowed?: boolean;
  denyReason?: string;
  actions?: PlanActionDTO[];
};

export type AgentSpawnMode = "single" | "parallel" | "chain";
export type AgentSpawnStatus =
  | "waiting"
  | "running"
  | "completed"
  | "failed"
  | "canceled"
  | "skipped"
  | "unknown"
  | "partial";

export type AgentSpawnRunDTO = {
  id: string;
  index: number;
  agent?: string;
  profile?: string;
  agentSource?: string;
  task: string;
  requestedModel?: string;
  // The following fields are supplied by ChatBlockSubagent.runs full snapshots.
  model?: string;
  status?: AgentSpawnStatus;
  lastToolName?: string;
  toolUses?: number;
  summary?: string;
  errorMessage?: string;
};

export type AgentSpawnDTO = {
  taskId: string;
  subagentType?: string;
  taskDescription?: string;
  prompt?: string;
  // model 有两个互斥来源:入参别名(静态,如 "haiku")或子代理首帧实际模型
  // (运行时,如 "claude-haiku-4-5-20251001")。card.tsx 的 readSpawn 用既有
  // 「运行时非空覆盖静态」语义在渲染前解出最终值,这里只存原值,不做归一化。
  model?: string;
  mode?: AgentSpawnMode;
  runs?: AgentSpawnRunDTO[];
  lastToolName?: string;
  toolUses?: number;
  totalTokens?: number;
  durationMs?: number;
  status?: AgentSpawnStatus;
};

export type ToolPermissionDTO = {
  requestId: string;
  toolName: string;
  toolInput?: Record<string, unknown>;
  resolved?: boolean;
  allowed?: boolean;
  alwaysAllow?: boolean;
};

export type CanonicalDTO =
  | { kind: "file.write"; fileWrite: FileWriteDTO }
  | { kind: "file.edit"; fileEdit: FileEditDTO }
  | { kind: "user.ask"; userAsk: UserAskDTO }
  | { kind: "plan.update"; planUpdate: PlanUpdateDTO }
  | { kind: "plan.approve_request"; planApprove: PlanApproveRequestDTO }
  | { kind: "agent.spawn"; agentSpawn: AgentSpawnDTO }
  | { kind: "tool.permission"; toolPermission: ToolPermissionDTO };
