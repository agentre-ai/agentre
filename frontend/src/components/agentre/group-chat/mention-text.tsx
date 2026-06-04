import * as React from "react";

import { cn } from "@/lib/utils";

// MentionText 是「消息正文」的纯展示渲染器：把正文里的 @mention 高亮成可点击 chip，
// 点击跳转到对应成员的会话视图。它只做高亮 + 跳转 —— 真正的「谁收到这条消息」是后端
// 结构化路由的事（recipientMemberIDs），不靠正文文本解析。正文是动态内容，绝不进 t()。
export type RosterEntry = { memberId: number; name: string };

export type MentionTextProps = {
  text: string;
  roster: RosterEntry[];
  onJump: (memberId: number) => void;
};

// MENTION_TOKEN 同时吃两种写法：
//   - <mention>NAME</mention> 标记（后端/编排器可能下发的结构化标记）
//   - 裸 @NAME（用户在 composer 里直接打的）
// 先把 <mention>X</mention> 归一成 @X，再按 @NAME 切分，逐段判断是否命中 roster。
const MENTION_MARKUP = /<mention>([^<]+)<\/mention>/g;

// @NAME：NAME 取到下一个空白/标点前。允许中英文、数字、下划线、连字符。
// 用捕获组切分，使得 split 结果里 mention 名字单独成段，便于逐段判断。
const MENTION_TOKEN = /@([^\s@<>,.!?，。！？、；：]+)/g;

function normalizeMarkup(text: string): string {
  return text.replace(MENTION_MARKUP, (_m, name: string) => `@${name}`);
}

function MentionText({ text, roster, onJump }: MentionTextProps) {
  const byName = React.useMemo(() => {
    const map = new Map<string, number>();
    for (const entry of roster) map.set(entry.name, entry.memberId);
    return map;
  }, [roster]);

  const normalized = React.useMemo(() => normalizeMarkup(text), [text]);

  // split 用带捕获组的正则：偶数下标是纯文本段，奇数下标是 @ 后的名字。
  const parts = React.useMemo(
    () => normalized.split(MENTION_TOKEN),
    [normalized],
  );

  return (
    <>
      {parts.map((part, idx) => {
        // 奇数下标 = 捕获组（mention 名字）。命中 roster → chip，否则回退成 "@名字" 纯文本。
        if (idx % 2 === 1) {
          const memberId = byName.get(part);
          if (memberId != null) {
            return (
              <button
                key={idx}
                type="button"
                onClick={() => onJump(memberId)}
                className={cn(
                  "rounded bg-primary/10 px-1 font-medium text-primary",
                  "hover:bg-primary/20",
                )}
              >
                @{part}
              </button>
            );
          }
          // 未命中：还原成原始的 "@名字" 纯文本（不是 chip，不可点）。
          return <React.Fragment key={idx}>@{part}</React.Fragment>;
        }
        if (!part) return null;
        return <React.Fragment key={idx}>{part}</React.Fragment>;
      })}
    </>
  );
}

export { MentionText };
