import * as React from "react";
import { useTranslation } from "react-i18next";
import { Check, ShieldAlert, X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import type { ToolApprovalData } from "@/stores/chat-streams-store";
import { useGroupListStore } from "@/stores/group-list-store";
import { AnswerToolApproval } from "../../../../wailsjs/go/app/App";
import { chat_svc } from "../../../../wailsjs/go/models";

// ToolApprovalCard 渲染 agent 内置写工具(org_create_department / org_update_agent /
// group_create / ...)的审批卡。视觉对齐 canonical-tool/tool-permission/card.tsx,但走
// 独立组件,按 block.type==="tool_approval" 直接路由(不进 CanonicalToolRouter)。
// toolKey 标识来源工具,供标题/文案与 approved 后处理选择。
//
// status 自身就是 truth:
//   - "pending":渲染入参 pre 块 + 批准/拒绝按钮
//   - "approved"|"denied"|"expired":渲染只读徽标 + result 文本(动态内容原样展示)
// 后端 finalize 已把悬空 pending 落成 expired,前端不按会话活跃度自行推断。
export const ToolApprovalCard: React.FC<{
  approval: ToolApprovalData;
  sessionId: number;
}> = ({ approval, sessionId }) => {
  const { t } = useTranslation();
  const [submitting, setSubmitting] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  const isPending = approval.status === "pending";
  const isApproved = approval.status === "approved";

  // group_create 批准落地后,侧栏群列表要立刻出现新群(fake/e2e 不产 tool block,
  // 刷新只能挂在审批卡上);reload 自带并发去重,历史卡重挂载多刷一次无害。
  React.useEffect(() => {
    if (approval.toolKey === "group_create" && isApproved) {
      void useGroupListStore.getState().reload();
    }
  }, [approval.toolKey, isApproved]);

  const answer = async (allow: boolean) => {
    if (!approval.requestId || submitting) return;
    setError(null);
    setSubmitting(true);
    try {
      // 所有内置写工具审批统一走 chat_svc 通用网关,按 requestId 路由唤醒。
      await AnswerToolApproval(
        chat_svc.AnswerToolApprovalRequest.createFrom({
          sessionId,
          requestId: approval.requestId,
          allow,
        }),
      );
    } catch {
      // 应答失败:切回可重试态并把错误文案露出来(对齐 tool-permission 卡的内联 error
      // 呈现,不用 toast)。决议落库与唤醒挂起 MCP 调用由后端保证。
      setError(t("toolApproval.submitFailed"));
      setSubmitting(false);
    }
  };

  const inputJson = approval.toolInput
    ? JSON.stringify(approval.toolInput, null, 2)
    : "";

  return (
    <div
      data-testid="tool-approval-card"
      data-selectable-text="true"
      className={cn(
        "rounded-md border bg-card text-card-foreground shadow-sm",
        !isPending && !isApproved
          ? "border-destructive/40"
          : "border-amber-500/40",
      )}
    >
      <div className="flex items-center gap-2 px-3 py-2">
        <ShieldAlert
          className={cn(
            "h-4 w-4 shrink-0",
            isPending
              ? "text-amber-500"
              : isApproved
                ? "text-emerald-500"
                : "text-destructive",
          )}
        />
        <span data-copyable-control-text="true" className="font-medium">
          {t(`toolApproval.tools.${approval.toolName}`, {
            defaultValue: approval.toolName,
          })}
        </span>
        <span className="text-xs text-muted-foreground">
          {t("toolApproval.title")}
        </span>
        {!isPending && (
          <span
            data-copyable-control-text="true"
            className={cn(
              "ml-auto rounded px-1.5 py-0.5 text-xs",
              isApproved
                ? "bg-emerald-500/10 text-emerald-600"
                : "bg-destructive/10 text-destructive",
            )}
          >
            {t(`toolApproval.status.${approval.status}`)}
          </span>
        )}
      </div>

      {isPending && inputJson && (
        <pre className="max-h-64 overflow-auto border-t border-border bg-muted/40 px-3 py-2 text-xs">
          <code>{inputJson}</code>
        </pre>
      )}

      {isPending ? (
        <div className="flex flex-wrap items-center gap-2 border-t border-border px-3 py-2">
          <Button size="sm" disabled={submitting} onClick={() => answer(true)}>
            <Check className="mr-1 h-3.5 w-3.5" />
            {t("toolApproval.approve")}
          </Button>
          <Button
            size="sm"
            variant="outline"
            disabled={submitting}
            onClick={() => answer(false)}
          >
            <X className="mr-1 h-3.5 w-3.5" />
            {t("toolApproval.deny")}
          </Button>
          {error && <span className="text-xs text-destructive">{error}</span>}
        </div>
      ) : approval.result ? (
        <div className="border-t border-border px-3 py-2 text-xs text-muted-foreground">
          {approval.result}
        </div>
      ) : null}
    </div>
  );
};
