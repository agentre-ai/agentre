import * as React from "react";
import { AnswerToolPermission } from "../../../../../wailsjs/go/app/App";
import { Button } from "@/components/ui/button";

// ToolPermissionOverlay 是 RawToolCard 的"等待审批"小条。ExitPlanMode 这种特例
// 走 plan-approve-request/card.tsx,不走这里;这里只负责通用工具的 Allow / Deny。
export type ToolPermissionPayload = {
  requestId: string;
  toolName?: string;
};

export const ToolPermissionOverlay: React.FC<{
  payload: ToolPermissionPayload;
  sessionId?: number;
}> = ({ payload, sessionId }) => {
  const [submitting, setSubmitting] = React.useState(false);

  const handle = async (allow: boolean) => {
    if (!sessionId || submitting) return;
    setSubmitting(true);
    try {
      await AnswerToolPermission({
        sessionId,
        requestId: payload.requestId,
        allow,
      } as Parameters<typeof AnswerToolPermission>[0]);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div
      className="mt-2 flex items-center gap-2 rounded border border-status-waiting/40 bg-status-waiting-bg px-2 py-1 text-xs"
      data-testid="tool-permission-overlay"
    >
      <span className="text-status-waiting">等待审批…</span>
      <Button
        size="sm"
        variant="default"
        disabled={submitting || !sessionId}
        onClick={() => void handle(true)}
      >
        允许
      </Button>
      <Button
        size="sm"
        variant="destructive"
        disabled={submitting || !sessionId}
        onClick={() => void handle(false)}
      >
        拒绝
      </Button>
    </div>
  );
};
