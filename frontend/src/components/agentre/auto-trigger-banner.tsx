import * as React from "react";
import { useTranslation } from "react-i18next";
import { BellRing } from "lucide-react";

// AutoTriggerBanner 渲染 transcript 内嵌的「后台任务完成 · 自动继续」分隔卡片 ——
// 标记一条**非用户发起**的 assistant 轮:CLI 在 run_in_background 任务完成后自主跑
// 的一轮(无 user 行)。它解释「为什么凭空多出一条 assistant 消息」。
//
// 视觉对齐 CompactBoundaryDivider(左右细线 + 中间 chip),与设计稿
// (~/Desktop/agentry.pen「Autonomous Turn」)一致。
export function AutoTriggerBanner(): React.ReactElement {
  const { t } = useTranslation();
  return (
    <div
      className="flex items-center gap-3 py-1"
      role="separator"
      aria-label={t("chatPanel.autonomous.aria")}
    >
      <div className="h-px flex-1 bg-border" />
      <div className="flex items-center gap-1.5 rounded-full border border-border bg-muted px-3 py-1 text-xs text-muted-foreground">
        <BellRing className="size-3 text-primary" aria-hidden="true" />
        <span className="font-medium">{t("chatPanel.autonomous.banner")}</span>
      </div>
      <div className="h-px flex-1 bg-border" />
    </div>
  );
}
