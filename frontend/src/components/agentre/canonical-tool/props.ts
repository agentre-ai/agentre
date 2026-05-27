// Shared props for canonical card components. Kept in a separate file so card
// modules can import it without pulling in the registry (which itself imports
// every card — would cause a circular import otherwise).

import type { ChatBlockData } from "@/stores/chat-streams-store";

export type PlanActionStream = {
  sessionId: number;
  userMessageId: number;
  assistantMessageId: number;
  stream: string;
};

export type CanonicalCardProps = {
  toolBlock: ChatBlockData;
  resultBlock?: ChatBlockData;
  cwd?: string;
  sessionId?: number;
  onPlanActionStarted?: (stream: PlanActionStream, userText: string) => void;
  // childBlocks 仅 agent.spawn 用 — 父 Agent/Task 工具下挂的内层 tool_use / tool_result
  // (parentToolUseId 已经在 chat.tsx 归集);AgentSpawnCard 展开后渲染为 STEPS 段。
  childBlocks?: ChatBlockData[];
};
