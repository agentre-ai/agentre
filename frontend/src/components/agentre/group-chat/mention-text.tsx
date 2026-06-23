import * as React from "react";

import { cn } from "@/lib/utils";

import type { MarkdownInlineDecorator } from "../markdown-text";

import { tokenizeMentions, type MentionRosterEntry } from "./mentions";

// MentionText 是「消息正文」的纯展示渲染器：把正文里的 @mention 高亮成可点击 chip，
// 点击跳转到对应成员的会话视图。它只做高亮 + 跳转 —— 真正的「谁收到这条消息」是后端
// 结构化路由的事（recipientMemberIDs），不靠正文文本解析。正文是动态内容，绝不进 t()。
//
// 解析逻辑统一委托给共享的 tokenizeMentions（精确、最长优先、带边界、注入安全），与
// composer 派生 recipientMemberIDs 同源，避免两边对「什么算一个 mention」各执一词。
export type RosterEntry = MentionRosterEntry;

export type MentionTextProps = {
  text: string;
  roster: RosterEntry[];
  onJump: (memberId: number) => void;
};

// MentionChip：@mention 的可点击 chip。MentionText（纯文本路径）与
// mentionMarkdownDecorator（markdown 路径）共用，样式只此一处。
function MentionChip({
  memberId,
  name,
  onJump,
}: {
  memberId: number;
  name: string;
  onJump: (memberId: number) => void;
}) {
  return (
    <button
      type="button"
      onClick={() => onJump(memberId)}
      className={cn(
        "rounded bg-primary/10 px-1 font-medium text-primary",
        "hover:bg-primary/20",
      )}
    >
      @{name}
    </button>
  );
}

function MentionText({ text, roster, onJump }: MentionTextProps) {
  const segments = React.useMemo(
    () => tokenizeMentions(text, roster),
    [text, roster],
  );

  return (
    <>
      {segments.map((seg, idx) => {
        if (seg.type === "mention") {
          return (
            <MentionChip
              key={idx}
              memberId={seg.memberId}
              name={seg.name}
              onJump={onJump}
            />
          );
        }
        return <React.Fragment key={idx}>{seg.value}</React.Fragment>;
      })}
    </>
  );
}

type MentionTokenData = { memberId: number; name: string };

// mentionMarkdownDecorator：把 mention 高亮装到 MarkdownText 的内联装饰器接缝上。
// tokenize 复用 tokenizeMentions（与 composer / MentionText 同一解析真相），装饰发生
// 在 markdown 文本节点层面，代码块里的 @name 由接缝天然跳过。调用方须用 useMemo 持有
// 稳定引用，否则 MarkdownText 的 memo 失效。
function mentionMarkdownDecorator(
  roster: RosterEntry[],
  onJump: (memberId: number) => void,
): MarkdownInlineDecorator<MentionTokenData> {
  return {
    tokenize: (text) =>
      tokenizeMentions(text, roster).map((seg) =>
        seg.type === "mention"
          ? {
              type: "token" as const,
              data: { memberId: seg.memberId, name: seg.name },
            }
          : seg,
      ),
    render: (data) => (
      <MentionChip memberId={data.memberId} name={data.name} onJump={onJump} />
    ),
  };
}

export { MentionChip, MentionText, mentionMarkdownDecorator };
