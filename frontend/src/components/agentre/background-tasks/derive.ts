import type { ChatBlockData } from "@/stores/chat-streams-store";

import type { chat_svc } from "../../../../wailsjs/go/models";

import type {
  BackgroundTask,
  BackgroundTaskKind,
  BackgroundTaskStatus,
} from "./types";

// deriveBackgroundTasks 从历史消息 + 当前 live blocks 中提取所有后台任务。
// 后台/subagent 任务 = type==="tool_use" 且 .subagent 存在的 block。
// 按 toolUseId dedupe：live 覆盖 history（live 更新）。
// VisitableBlock 是 visit 只需读取的最小结构投影。subagent 直接复用生成的
// ChatBlockSubagent，让 ChatBlockData（subagent: ChatBlockSubagent）无需 cast
// 即可传入；持久化 chat_svc.ChatBlock 走双重 cast 投影。
type VisitableBlock = {
  type?: string;
  toolUseId?: string;
  subagent?: chat_svc.ChatBlockSubagent;
};

export function deriveBackgroundTasks(
  messages: chat_svc.ChatMessage[],
  liveBlocks: ChatBlockData[],
): BackgroundTask[] {
  const byId = new Map<string, BackgroundTask>();

  const visit = (block: VisitableBlock | undefined) => {
    if (!block || block.type !== "tool_use") return;
    const sa = block.subagent;
    const toolUseId = block.toolUseId;
    if (!toolUseId || !sa) return;
    byId.set(toolUseId, {
      toolUseId,
      kind: mapKind(sa.kind),
      description: sa.taskDescription ?? "",
      status: mapStatus(sa.status),
    });
  };

  // 先处理历史消息 (history)，再处理 live blocks (live wins on conflict)。
  // 历史 block 是 chat_svc.ChatBlock，走 task-progress/derive 同款双重 cast
  // 投影到 VisitableBlock。
  for (const m of messages) {
    for (const b of m.blocks ?? []) visit(b as unknown as VisitableBlock);
  }
  // liveBlocks 是 ChatBlockData，结构性满足 VisitableBlock，无需 cast。
  for (const b of liveBlocks) visit(b);

  return [...byId.values()];
}

function mapKind(raw: string | undefined): BackgroundTaskKind {
  return raw === "local_bash" ? "local_bash" : "local_agent";
}

function mapStatus(raw: string | undefined): BackgroundTaskStatus {
  if (raw === "completed") return "completed";
  if (raw === "failed") return "failed";
  return "running";
}
