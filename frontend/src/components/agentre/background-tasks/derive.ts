import type { ChatBlockData } from "@/stores/chat-streams-store";

import type { chat_svc } from "../../../../wailsjs/go/models";

import type { BackgroundTask, BackgroundTaskStatus } from "./types";

// deriveBackgroundTasks 从历史消息 + 当前 live blocks 中提取所有后台任务。
// 只收真正的后台 bash：kind==="local_bash" **且** 工具入参 run_in_background===true。
// 真实 CLI 对*每一次* Bash 都发 task_type:"local_bash" 帧(不只是后台 bash),所以
// 光看 kind 会把所有前台 bash 也收进来 —— 唯一可靠的「后台」信号是 Bash 入参的
// run_in_background(与 RawToolCard 内联「后台运行」pill 同款判据)。local_agent
// subagent 整体排除。按 toolUseId dedupe：live 覆盖 history（live 更新）。
// VisitableBlock 是 visit 只需读取的最小结构投影。subagent 直接复用生成的
// ChatBlockSubagent，让 ChatBlockData（subagent: ChatBlockSubagent）无需 cast
// 即可传入；持久化 chat_svc.ChatBlock 走双重 cast 投影。toolInput 是工具 raw 入参
// （wire DTO view.ChatBlock.toolInput / live ChatBlockData.toolInput）。
type VisitableBlock = {
  type?: string;
  toolUseId?: string;
  toolInput?: Record<string, unknown>;
  subagent?: chat_svc.ChatBlockSubagent;
};

export function deriveBackgroundTasks(
  messages: chat_svc.ChatMessage[],
  liveBlocks: ChatBlockData[],
  clearedToolUseIds?: ReadonlySet<string>,
): BackgroundTask[] {
  const byId = new Map<string, BackgroundTask>();

  const visit = (block: VisitableBlock | undefined, startedAt?: number) => {
    if (!block || block.type !== "tool_use") return;
    const sa = block.subagent;
    const toolUseId = block.toolUseId;
    if (!toolUseId || !sa) return;
    // 只收 run_in_background bash;subagent(local_agent)整体排除(真 CLI 无法区分
    // 前台/后台 subagent,产品决策只展示真正后台的 bash 任务)。
    if (sa.kind !== "local_bash") return;
    // CLI 对每一次 Bash 都发 local_bash 帧,kind 不足以判定「后台」;唯一可靠信号是
    // 工具入参 run_in_background===true(前台 bash 无此入参,被这里排除)。
    if (block.toolInput?.run_in_background !== true) return;
    if (clearedToolUseIds?.has(toolUseId)) return;
    const prev = byId.get(toolUseId);
    byId.set(toolUseId, {
      toolUseId,
      taskId: sa.taskId || prev?.taskId,
      kind: "local_bash",
      description: sa.taskDescription ?? prev?.description ?? "",
      status: mapStatus(sa.status),
      startedAt: startedAt ?? prev?.startedAt,
      durationMs:
        (typeof sa.durationMs === "number" ? sa.durationMs : undefined) ??
        prev?.durationMs,
      summary: (sa.summary || undefined) ?? prev?.summary,
    });
  };

  // 先处理历史消息 (history)，再处理 live blocks (live wins on conflict)。
  // 历史 block 是 chat_svc.ChatBlock，走 task-progress/derive 同款双重 cast
  // 投影到 VisitableBlock。startedAt 取自消息的 createtime (epoch ms)。
  for (const m of messages) {
    for (const b of m.blocks ?? [])
      visit(b as unknown as VisitableBlock, m.createtime);
  }
  // liveBlocks 是 ChatBlockData，结构性满足 VisitableBlock，无需 cast。
  // live blocks 没有关联的消息，所以 startedAt 为 undefined。
  for (const b of liveBlocks) visit(b);

  return [...byId.values()];
}

function mapStatus(raw: string | undefined): BackgroundTaskStatus {
  if (raw === "completed") return "completed";
  if (raw === "failed") return "failed";
  if (raw === "canceled") return "failed";
  return "running";
}
