import type { chat_svc } from "../../../../wailsjs/go/models";

export interface SubagentLite {
  toolUseId: string;
  role: string;
  description: string;
  status: "running" | "completed" | "failed";
}

type VisitableBlock = {
  type?: string;
  toolUseId?: string;
  subagent?: chat_svc.ChatBlockSubagent;
};

// deriveSubagents 从一个 session 的历史消息里收 CLI 子代理(Task 工具)。
// 与后台任务面板的 deriveBackgroundTasks 对称:那边收 local_bash、排除 subagent;
// 这边收 local_agent(CLI 子代理)、排除 local_bash。按 toolUseId dedupe(后覆盖)。
export function deriveSubagents(
  messages: chat_svc.ChatMessage[],
): SubagentLite[] {
  const byId = new Map<string, SubagentLite>();
  for (const m of messages) {
    for (const b of m.blocks ?? []) {
      const block = b as unknown as VisitableBlock;
      if (block.type !== "tool_use") continue;
      const sa = block.subagent;
      const id = block.toolUseId;
      if (!sa || !id) continue;
      if (sa.kind !== "local_agent") continue;
      byId.set(id, {
        toolUseId: id,
        role: sa.subagentType || sa.taskDescription || "",
        description: sa.taskDescription || "",
        status: mapStatus(sa.status),
      });
    }
  }
  return [...byId.values()];
}

function mapStatus(raw: string | undefined): SubagentLite["status"] {
  if (raw === "completed") return "completed";
  if (raw === "failed" || raw === "canceled") return "failed";
  return "running";
}
