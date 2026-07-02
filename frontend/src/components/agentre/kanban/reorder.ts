import type { app } from "../../../../wailsjs/go/models";
import { STAGES, type StageId } from "./stages";

export function groupByStage(
  issues: app.IssueItem[],
): Record<StageId, app.IssueItem[]> {
  const out = { todo: [], doing: [], review: [], done: [] } as Record<
    StageId,
    app.IssueItem[]
  >;
  for (const it of issues) {
    const stage = (it.stage || "todo") as StageId;
    (out[stage] ?? out.todo).push(it);
  }
  for (const s of STAGES) {
    out[s.id].sort((a, b) => a.position - b.position);
  }
  return out;
}

// afterIdForDrop：给定卡片将插入的目标索引 overIndex（0..len），
// 返回其前一张卡的 id（0 = 落在列顶）。
export function afterIdForDrop(
  list: app.IssueItem[],
  overIndex: number,
): number {
  if (overIndex <= 0) return 0;
  const prev = list[Math.min(overIndex, list.length) - 1];
  return prev ? prev.id : 0;
}
