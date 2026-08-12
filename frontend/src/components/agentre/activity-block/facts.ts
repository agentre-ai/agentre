// facts.ts — 活动行渲染需要的「这一步实际做了什么」的纯提取层。零 JSX,便于单测。
//
// 判据本身不在这里:三档权重来自 canonical-tool/tier 的 tier(),图标分类来自
// toolCategory() —— 全程不查工具名表(raw/summary.ts 立下的规矩)。这里只做两件
// 事:把 canonical / result 里已有的事实(增删行、退出码、结果规模)取出来,以及
// 决定这一步用哪一档字重。

import type { ChatBlockData } from "@/stores/chat-streams-store";

import { tier } from "../canonical-tool/tier";
import type { CanonicalDTO } from "../canonical-tool/types";
import type { ActivityStep } from "../transcript-rows";

/** 三档视觉权重:读最轻、中性居中、写最重。出组档不会进活动块,兜底按中性处理。 */
export type ActivityWeight = "neutral" | "read" | "write";

export function stepWeight(step: ActivityStep): ActivityWeight {
  // 思考与只读探查同为「不改变工程」的活动,取最轻的一档。
  if (step.type === "thinking") return "read";
  const block = step.toolBlock;
  if (!block) return "neutral";
  const t = tier(block);
  return t === "read" || t === "write" ? t : "neutral";
}

export function canonicalOf(block?: ChatBlockData): CanonicalDTO | undefined {
  return (block as { canonical?: CanonicalDTO } | undefined)?.canonical;
}

/** 一步执行完之后可报出来的事实。空值一律表示「这一步没有这项事实」。 */
export type StepFacts = {
  /** 结果被标成错误 —— 与组头红色失败计数同一判据(resultBlock.isError)。 */
  failed: boolean;
  /** 非零退出码。命令类结果的 JSON 形状里才有。 */
  exitCode?: number;
  /** file.edit / file.write 的增删行。 */
  plus?: number;
  minus?: number;
  /** 展开体要渲染的结果正文(命令结果取 output,其余取原文)。 */
  resultText: string;
  /** 单行结果的预览(多行结果改报规模)。 */
  preview?: string;
  /** 多行结果的行数 —— 折叠态报规模而不是把整段结果塞进行里。 */
  lines?: number;
};

export function stepFacts(step: ActivityStep): StepFacts {
  if (step.type === "thinking") {
    const text = step.block.text ?? "";
    return { failed: false, resultText: text };
  }
  const result = step.resultBlock;
  const command = parseCommandResult(result?.text);
  const resultText = command ? command.output : (result?.text ?? "");
  const canonical = canonicalOf(step.toolBlock);
  const facts: StepFacts = {
    exitCode:
      typeof command?.exitCode === "number" && command.exitCode !== 0
        ? command.exitCode
        : undefined,
    failed: !!result?.isError,
    resultText,
  };
  if (canonical?.kind === "file.edit") {
    const files = canonical.fileEdit?.files ?? [];
    facts.plus = files.reduce((a, f) => a + (f.plus ?? 0), 0);
    facts.minus = files.reduce((a, f) => a + (f.minus ?? 0), 0);
    return facts;
  }
  if (canonical?.kind === "file.write") {
    facts.plus = canonical.fileWrite?.lines ?? 0;
    return facts;
  }
  const trimmed = resultText.trim();
  if (!trimmed) return facts;
  const lineCount = trimmed.split("\n").length;
  if (lineCount > 1) facts.lines = lineCount;
  else facts.preview = trimmed;
  return facts;
}

type CommandResult = { exitCode?: number; output: string; status?: string };

// parseCommandResult 与 canonical-tool/raw/card.tsx 的同名解析同源:command_execution
// 类工具的 tool_result 是 JSON {exitCode, output, status},其余工具是纯文本。
// **靠 result shape 判定**,不靠 toolName。
function parseCommandResult(text?: string): CommandResult | null {
  if (typeof text !== "string") return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== "object") return null;
  const data = parsed as Record<string, unknown>;
  if (!("output" in data) && !("exitCode" in data) && !("status" in data)) {
    return null;
  }
  return {
    exitCode: typeof data.exitCode === "number" ? data.exitCode : undefined,
    output: stringifyOutput(data.output),
    status: typeof data.status === "string" ? data.status : undefined,
  };
}

function stringifyOutput(output: unknown): string {
  if (output == null) return "";
  if (typeof output === "string") return output;
  try {
    return JSON.stringify(output, null, 2);
  } catch {
    return String(output);
  }
}
