import * as React from "react";

import { cn } from "@/lib/utils";

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
            <button
              key={idx}
              type="button"
              onClick={() => onJump(seg.memberId)}
              className={cn(
                "rounded bg-primary/10 px-1 font-medium text-primary",
                "hover:bg-primary/20",
              )}
            >
              @{seg.name}
            </button>
          );
        }
        return <React.Fragment key={idx}>{seg.value}</React.Fragment>;
      })}
    </>
  );
}

export { MentionText };
