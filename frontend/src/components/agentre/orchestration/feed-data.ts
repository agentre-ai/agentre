import type { app } from "../../../../wailsjs/go/models";

export interface FeedItem {
  id: string;
  kind: "dispatch" | "report" | "finish" | "blocked" | "ask";
  agentId: number;
  text: string;
  ts: number;
}

export function buildFeed(detail: app.RunDetailDTO): FeedItem[] {
  const items: FeedItem[] = [];
  for (const t of detail.tasks ?? []) {
    if (t.parentTaskId) {
      items.push({
        id: `d${t.id}`,
        kind: "dispatch",
        agentId: t.agentId,
        text: t.brief,
        ts: t.createtime ?? 0,
      });
    }
    if (t.status === "done" && (t.result ?? "").trim()) {
      items.push({
        id: `r${t.id}`,
        kind: "report",
        agentId: t.agentId,
        text: t.result,
        ts: t.updatetime ?? 0,
      });
    }
    if (t.status === "error") {
      items.push({
        id: `e${t.id}`,
        kind: "blocked",
        agentId: t.agentId,
        text: t.result || "",
        ts: t.updatetime ?? 0,
      });
    }
  }
  return items.sort((a, b) => a.ts - b.ts);
}
