import type { app } from "../../../../wailsjs/go/models";

export interface FeedItem {
  id: string;
  kind: "dispatch" | "report" | "finish" | "blocked" | "ask" | "reply";
  agentId: number;
  targetAgentId?: number;
  text: string;
  ts: number;
}

export interface AskLogItem {
  kind: "ask" | "reply";
  askId: string;
  agentId: number;
  targetAgentId?: number;
  text: string;
  ts: number;
}

export function buildFeed(
  detail: app.RunDetailDTO,
  askLog: AskLogItem[] = [],
): FeedItem[] {
  const items: FeedItem[] = [];
  for (const t of detail.dispatches ?? []) {
    if (t.parentDispatchId) {
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
  for (const a of askLog) {
    items.push({
      id: `${a.kind}-${a.askId}`,
      kind: a.kind,
      agentId: a.agentId,
      targetAgentId: a.targetAgentId,
      text: a.text,
      ts: a.ts,
    });
  }
  return items.sort((a, b) => a.ts - b.ts);
}
