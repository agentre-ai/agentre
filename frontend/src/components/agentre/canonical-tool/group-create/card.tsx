import * as React from "react";
import { useTranslation } from "react-i18next";
import { ArrowRight, Users } from "lucide-react";

import { Button } from "@/components/ui/button";
import { useChatTabsStore } from "@/stores/chat-tabs-store";

import type { CanonicalCardProps } from "../props";
import { RawToolCard } from "../raw/card";

// 后端 group MCP create.go 的成功 result 文本契约(锚定注释见后端):
// `group created: id=<id> title=<title>`。审批被拒/超时/执行失败时 result 是
// 别的文本(「用户拒绝了此操作」等) → parse 失败 → 回退 RawToolCard 原样展示。
const RESULT_RE = /^group created: id=(\d+) title=(.*)$/;

export function parseGroupCreateResult(
  text: string | undefined,
): { id: number; title: string } | null {
  const m = text?.match(RESULT_RE);
  return m ? { id: Number(m[1]), title: m[2] } : null;
}

// GroupCreateCard 把 group_create 工具的成功 result 渲染成「已创建群聊 →」
// 跳转卡;点按钮经 chat-tabs-store.openGroup 打开(或聚焦)对应群 tab。
export const GroupCreateCard: React.FC<CanonicalCardProps> = (props) => {
  const { t } = useTranslation();
  const openGroup = useChatTabsStore((s) => s.openGroup);
  const parsed = parseGroupCreateResult(props.resultBlock?.text);
  if (!parsed) return <RawToolCard {...props} />;
  return (
    <section
      data-testid="group-create-card"
      aria-label={t("groupCreateCard.created")}
      className="flex w-full max-w-[720px] items-center gap-2 rounded-md border border-border bg-card px-3 py-2 font-mono text-xs"
    >
      <Users
        className="size-3.5 shrink-0 text-primary-text"
        aria-hidden="true"
      />
      <span className="shrink-0 font-semibold text-primary-text">
        {t("groupCreateCard.created")}
      </span>
      <span className="text-muted-foreground">·</span>
      <span className="min-w-0 truncate text-muted-foreground">
        {parsed.title}
      </span>
      <span className="min-w-0 flex-1" />
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className="h-6 shrink-0 gap-1 px-2 text-xs"
        onClick={() => openGroup(parsed.id, parsed.title)}
      >
        {t("groupCreateCard.open")}
        <ArrowRight className="size-3" aria-hidden="true" />
      </Button>
    </section>
  );
};
