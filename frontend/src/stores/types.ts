// frontend/src/stores/types.ts
//
// 集中定义 session 投影类型，消除各文件内的分散定义。
// 各 store / hook 从此文件 re-export，消费方统一从 @/stores/types 导入。
//
// 纯类型文件：0 运行时代码。

// AgentStatus: session 级运行态 token。唯一定义；components/agentre/types.ts re-exports 此类型。
export type AgentStatus = "idle" | "running" | "waiting" | "error";

// SessionMetaSnapshot: session-meta-store 持有的静态字段快照。
// 等同于 session-meta-store.ts 的 SessionMeta，此处做 re-export 别名。
//
// 注：不含 id 字段 —— sessionId 是 session-meta-store.metas 这个 Map 的 key，
// 不在 value 里重复。spec §5.1 的字段表里写了 id 是笔误，实际以本类型为准。
export type SessionMetaSnapshot = {
  agentId: number;
  agentName: string;
  agentColor: string;
  projectId?: number;
  title: string;
  lastMessageAt?: number;
  lastReadAt?: number;
  // permissionModeAtLaunch 是 CLI spawn 时下发的快照,session 生命周期内不变。
  // PlanApproveCard 据此判断 ExitPlanMode 弹卡要不要给 bypass 选项。
  permissionModeAtLaunch?: string;
};

// SessionStatusPatch: session-status-store upsert 接受的入参类型。
// 与 session-status-store.ts 的 SessionStatusPatch 对齐（不含 doneTick/lastDoneEvent）。
export type SessionStatusPatch = {
  agentStatus: AgentStatus;
  needsAttention: boolean;
  permissionMode?: string;
};

// SessionView: useSessionWithOverlays 返回的合并投影。
// 包含 session 静态字段 + 运行时态 + 合并后的已读时间戳。
export type SessionView = {
  id: number;
  agentId: number;
  agentName: string;
  agentColor: string;
  projectId?: number;
  title: string;
  lastMessageAt: number;
  agentStatus: AgentStatus;
  needsAttention: boolean;
  permissionMode?: string;
  lastReadAt: number;
};

// AttentionInput: computeAttention 纯函数的输入类型。
export type AttentionInput = {
  agentStatus: AgentStatus;
  needsAttention: boolean;
  lastMessageAt: number;
  lastReadAt: number;
};

// AttentionReason: 4 种 attention 状态，平权。UI 层据此选色 / 文案。
export type AttentionReason =
  | "needs_attention"
  | "running"
  | "error"
  | "unread";

// ChatSessionStatusEvent: 后端 SSE 透传的 session_status 字段类型。
// 与 use-chat-stream.ts 的 ChatSessionStatusPatch 对齐。
// agentStatus 在后端/Wails 边界以 string 到达；cast 发生在写入 store 的调用侧。
export type ChatSessionStatusEvent = {
  agentStatus: string;
  needsAttention: boolean;
  permissionMode?: string;
  contextWindow?: number;
};
