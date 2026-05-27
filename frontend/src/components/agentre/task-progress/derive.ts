import type { ChatBlockData } from "@/stores/chat-streams-store";

import type { Task, TaskProgress, TaskStatus } from "./types";

import type { chat_svc } from "../../../../wailsjs/go/models";

// deriveTaskProgress 只读最新一条带 canonical.kind="plan.update" 的 block
// (`tool_use` / `plan` block 都可能携带,backend 收编 TodoWrite / TaskCreate+
// TaskUpdate / update_plan 后统一写在 canonical.PlanUpdate.steps 里)。
// 没有任何 plan canonical → 返空。
//
// 历史 derive 逻辑(re-parse raw tool input + tool_result.meta.task.id 重建
// claude/codex 各自的列表)已迁到 backend(claudecode task_aggregator + codex
// translator),前端不再分支 runtime。
export function deriveTaskProgress(
  messages: chat_svc.ChatMessage[],
  liveBlocks: ChatBlockData[],
): TaskProgress {
  const latest = findLatestPlanCanonical(messages, liveBlocks);
  if (!latest) return { tasks: [] };
  return { tasks: latest.steps.map(stepToTask) };
}

type RawPlanStep = {
  id?: string;
  step: string;
  status: string;
};

type RawPlanCanonical = { steps: RawPlanStep[] };

function findLatestPlanCanonical(
  messages: chat_svc.ChatMessage[],
  liveBlocks: ChatBlockData[],
): RawPlanCanonical | null {
  // 反向扫:先看 liveBlocks(本轮 in-flight),再看历史 messages 倒序。
  for (let i = liveBlocks.length - 1; i >= 0; i--) {
    const canonical = extractPlanCanonical(liveBlocks[i]);
    if (canonical) return canonical;
  }
  for (let i = messages.length - 1; i >= 0; i--) {
    const blocks = messages[i].blocks ?? [];
    for (let j = blocks.length - 1; j >= 0; j--) {
      const canonical = extractPlanCanonical(
        blocks[j] as unknown as ChatBlockData,
      );
      if (canonical) return canonical;
    }
  }
  return null;
}

function extractPlanCanonical(
  block: ChatBlockData | undefined,
): RawPlanCanonical | null {
  if (!block) return null;
  const canonical = (block as { canonical?: { kind?: string } }).canonical;
  if (!canonical || canonical.kind !== "plan.update") return null;
  const planUpdate = (canonical as { planUpdate?: { steps?: RawPlanStep[] } })
    .planUpdate;
  if (!planUpdate || !Array.isArray(planUpdate.steps)) return null;
  return { steps: planUpdate.steps };
}

function stepToTask(step: RawPlanStep, i: number): Task {
  return {
    id: step.id ?? `step-${i}`,
    description: step.step,
    status: mapStepStatus(step.status),
  };
}

function mapStepStatus(raw: string): TaskStatus {
  switch (raw) {
    case "completed":
      return "completed";
    case "inProgress":
      return "running";
    case "canceled":
      return "cancelled";
    default:
      return "queued";
  }
}
