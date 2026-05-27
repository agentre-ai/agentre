// frontend/src/hooks/use-session-with-overlays.ts
//
// useSessionWithOverlays 是「我需要 session 当前最新视图」的唯一入口。
// 内部合并 3 个 store：
//   - session-meta-store:   静态字段（agentName/agentColor/projectId/title/lastMessageAt/lastReadAt）
//   - session-status-store: 运行态（agentStatus/needsAttention/permissionMode）
//   - session-read-store:   lastReadAt overlay（max(server-stored, client-override)）
//
// 同值返回同引用（referential equality），下游 useMemo 可以安全用作依赖。
import * as React from "react";

import { useSessionMetaStore } from "@/stores/session-meta-store";
import { useSessionReadStore } from "@/stores/session-read-store";
import { useSessionStatusStore } from "@/stores/session-status-store";

// SessionView 类型定义已统一到 @/stores/types，此处 import + re-export。
import type { SessionView } from "@/stores/types";
export type { SessionView };

export function useSessionWithOverlays(sessionId: number): SessionView | null {
  const meta = useSessionMetaStore((s) =>
    sessionId > 0 ? (s.metas.get(sessionId) ?? null) : null,
  );
  const status = useSessionStatusStore((s) =>
    sessionId > 0 ? (s.statuses.get(sessionId) ?? null) : null,
  );
  const readOverride = useSessionReadStore((s) =>
    sessionId > 0 ? s.overrides.get(sessionId) : undefined,
  );

  return React.useMemo<SessionView | null>(() => {
    if (!meta) return null;
    // lastReadAt: max(server-stored via meta, client-override via session-read-store)
    const baseLastRead = meta.lastReadAt ?? 0;
    const lastReadAt = Math.max(baseLastRead, readOverride ?? 0);
    return {
      id: sessionId,
      agentId: meta.agentId,
      agentName: meta.agentName,
      agentColor: meta.agentColor,
      projectId: meta.projectId,
      title: meta.title,
      lastMessageAt: meta.lastMessageAt ?? 0,
      agentStatus: status?.agentStatus ?? "idle",
      needsAttention: status?.needsAttention ?? false,
      permissionMode: status?.permissionMode,
      lastReadAt,
    };
  }, [sessionId, meta, status, readOverride]);
}
