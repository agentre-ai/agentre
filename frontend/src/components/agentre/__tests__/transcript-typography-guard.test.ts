import fs from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

// 对话流排版护栏。不逐组件断言 class(那种测试与实现同构、无信息量),
// 只禁止若干已被 token 取代的字面量在对话流链路上复活。
//
// chat.tsx 同时装着 transcript 和 composer —— 646/654/714/726 行的圆角与阴影
// 属于输入框,不归对话流卡片系统管。所以规则按文件裁剪,不一刀切。
const AGENTRE_DIR = path.resolve(__dirname, "..");

type RuleGroup = "type" | "measure" | "shadow" | "radius";

const RULES: { group: RuleGroup; pattern: RegExp; why: string }[] = [
  { group: "type", pattern: /text-\[9px\]/, why: "低于可读下限,用 text-meta" },
  { group: "type", pattern: /text-\[10px\]/, why: "低于可读下限,用 text-meta" },
  { group: "measure", pattern: /max-w-\[720px\]/, why: "用 max-w-measure" },
  { group: "measure", pattern: /max-w-\[580px\]/, why: "用 max-w-measure" },
  { group: "shadow", pattern: /shadow-sm/, why: "对话流卡片不带阴影" },
  { group: "radius", pattern: /rounded-md/, why: "对话流卡片统一 rounded-lg" },
];

// SCANNED:对话流渲染链路上的组件。新增 transcript 组件时必须加进来 ——
// 让「加进对话流」成为一个需要过目排版护栏的动作。
// skip:该文件豁免的规则组,每一项都要写清理由。
export const SCANNED: { file: string; skip?: RuleGroup[] }[] = [
  { file: "transcript-card.tsx" },
  { file: "message-row.tsx" },
  { file: "markdown-text.tsx" },
  { file: "code-block.tsx" },
  { file: "thinking-block.tsx" },
  { file: "canonical-tool/raw/card.tsx" },
  { file: "canonical-tool/file-edit/card.tsx" },
  { file: "canonical-tool/file-edit/hunk-renderer.tsx" },
  { file: "canonical-tool/file-write/card.tsx" },
  { file: "canonical-tool/agent-spawn/card.tsx" },
  { file: "canonical-tool/plan/card.tsx" },
];

function violations(source: string, skip: RuleGroup[] = []): string[] {
  return RULES.filter(
    (r) => !skip.includes(r.group) && r.pattern.test(source),
  ).map((r) => `${r.pattern.source} —— ${r.why}`);
}

describe("transcript typography guard", () => {
  it("检测器能抓到违规源码", () => {
    expect(violations('<div className="text-[9px] rounded-md" />')).toEqual([
      "text-\\[9px\\] —— 低于可读下限,用 text-meta",
      "rounded-md —— 对话流卡片统一 rounded-lg",
    ]);
  });

  it("检测器尊重 skip", () => {
    expect(violations('<div className="rounded-md" />', ["radius"])).toEqual(
      [],
    );
  });

  it("对话流组件不含被禁的排版字面量", () => {
    const found = SCANNED.flatMap(({ file, skip }) => {
      const source = fs.readFileSync(path.join(AGENTRE_DIR, file), "utf8");
      return violations(source, skip).map((v) => `${file}: ${v}`);
    });
    expect(found).toEqual([]);
  });
});
