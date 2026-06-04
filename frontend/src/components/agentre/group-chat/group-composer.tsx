import * as React from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

import { parseMentionedMemberIds } from "./mentions";

import type { app } from "../../../../wailsjs/go/models";

type GroupMemberItem = app.GroupMemberItem;

export type GroupComposerSend = {
  text: string;
  recipientMemberIDs: number[];
};

export type GroupComposerProps = {
  members: GroupMemberItem[];
  memberName: (memberId: number) => string;
  onSend: (payload: GroupComposerSend) => void;
  disabled?: boolean;
};

// MentionState：@autocomplete 当前是否激活 + 触发位置 + 过滤前缀。
// trigger = textarea 里 '@' 的下标；query = '@' 之后到光标的子串（小写匹配成员名）。
type MentionState = {
  trigger: number;
  query: string;
} | null;

// detectMention 从「光标左侧的文本」回看最近的 '@'，若 '@' 到光标之间没有空白
// 就认为正在输入一个 mention，返回触发位置 + query。否则返回 null（关闭弹窗）。
function detectMention(text: string, caret: number): MentionState {
  const before = text.slice(0, caret);
  const at = before.lastIndexOf("@");
  if (at < 0) return null;
  const between = before.slice(at + 1);
  if (/\s/.test(between)) return null;
  return { trigger: at, query: between.toLowerCase() };
}

function GroupComposer({
  members,
  memberName,
  onSend,
  disabled,
}: GroupComposerProps) {
  const { t } = useTranslation();
  const [text, setText] = React.useState("");
  // 发送时的 recipientMemberIDs 直接解析最终文本里真实出现的 @name token（见 doSend /
  // parseMentionedMemberIds）——前端是结构化收件人的唯一来源，后端不做任何文本解析，
  // 所以收件人必须与正文里实际出现的 @mention 完全一致，不再维护一份独立的「已选」状态。
  const [mention, setMention] = React.useState<MentionState>(null);
  const [activeIdx, setActiveIdx] = React.useState(0);
  const textareaRef = React.useRef<HTMLTextAreaElement>(null);

  const activeMembers = members.filter((m) => m.status === "active");

  // roster：把成员映射成 { memberId, name }，供 parseMentionedMemberIds 在发送时从
  // 最终文本里解析出真正被 @ 到的收件人——与 transcript 的 chip 渲染同一套解析逻辑。
  const roster = React.useMemo(
    () => members.map((m) => ({ memberId: m.id, name: memberName(m.id) })),
    [members, memberName],
  );

  const suggestions = React.useMemo(() => {
    if (!mention) return [];
    const q = mention.query;
    return activeMembers.filter((m) =>
      memberName(m.id).toLowerCase().includes(q),
    );
  }, [mention, activeMembers, memberName]);

  // syncMention 是唯一改写 mention 状态的入口；query 变化时把高亮项重置回首项，
  // 不用额外的 effect（避免 set-state-in-effect 引起的级联渲染）。
  function syncMention(value: string, caret: number) {
    const next = detectMention(value, caret);
    if (next?.query !== mention?.query) setActiveIdx(0);
    setMention(next);
  }

  function handleChange(e: React.ChangeEvent<HTMLTextAreaElement>) {
    const value = e.target.value;
    setText(value);
    syncMention(value, e.target.selectionStart ?? value.length);
  }

  function insertMention(member: GroupMemberItem) {
    if (!mention) return;
    const name = memberName(member.id);
    const head = text.slice(0, mention.trigger);
    const caret = textareaRef.current?.selectionStart ?? text.length;
    const tail = text.slice(caret);
    const inserted = `${head}@${name} ${tail}`;
    setText(inserted);
    setMention(null);
    // 把焦点 + 光标放回插入点之后，方便用户接着打字。
    const nextCaret = head.length + name.length + 2;
    requestAnimationFrame(() => {
      const el = textareaRef.current;
      if (!el) return;
      el.focus();
      el.setSelectionRange(nextCaret, nextCaret);
    });
  }

  function doSend() {
    const trimmed = text.trim();
    if (!trimmed || disabled) return;
    // 收件人 = 最终文本里以 @name token 真实出现、且命中 roster 的成员（精确、最长优先、
    // 带边界）。这样删掉某个 @name 后它就不再是收件人，而 "@Bobby" 也不会误把 "Bob" 算进去。
    const recipientMemberIDs = parseMentionedMemberIds(trimmed, roster);
    onSend({ text: trimmed, recipientMemberIDs });
    setText("");
    setMention(null);
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (mention && suggestions.length > 0) {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setActiveIdx((i) => (i + 1) % suggestions.length);
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        setActiveIdx((i) => (i - 1 + suggestions.length) % suggestions.length);
        return;
      }
      if (e.key === "Enter" || e.key === "Tab") {
        e.preventDefault();
        insertMention(suggestions[activeIdx]);
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        setMention(null);
        return;
      }
    }
    if (e.key === "Enter" && !e.shiftKey && !e.metaKey && !e.ctrlKey) {
      e.preventDefault();
      doSend();
    }
  }

  const showSuggestions = mention !== null && suggestions.length > 0;

  return (
    <div className="relative border-t border-border bg-background px-5 py-3">
      {showSuggestions ? (
        <ul
          role="listbox"
          data-testid="group-mention-list"
          className="absolute bottom-full left-5 z-50 mb-1 max-h-48 w-56 overflow-auto rounded-lg border border-border bg-popover py-1 shadow-md"
        >
          {suggestions.map((m, idx) => (
            <li key={m.id}>
              <button
                type="button"
                role="option"
                aria-selected={idx === activeIdx}
                onMouseDown={(e) => {
                  e.preventDefault();
                  insertMention(m);
                }}
                onMouseEnter={() => setActiveIdx(idx)}
                className={cn(
                  "flex w-full items-center px-3 py-1.5 text-left text-sm",
                  idx === activeIdx
                    ? "bg-accent text-accent-foreground"
                    : "text-foreground hover:bg-accent/60",
                )}
              >
                {/* 成员名是动态内容，原样渲染，不进 t()。 */}@
                {memberName(m.id)}
              </button>
            </li>
          ))}
        </ul>
      ) : null}
      <div className="flex items-end gap-2">
        <Textarea
          ref={textareaRef}
          rows={2}
          value={text}
          disabled={disabled}
          onChange={handleChange}
          onKeyDown={handleKeyDown}
          onSelect={(e) =>
            syncMention(
              e.currentTarget.value,
              e.currentTarget.selectionStart ?? e.currentTarget.value.length,
            )
          }
          placeholder={t("group.composer.placeholder")}
          className="max-h-40 min-h-[44px] resize-none"
        />
        <Button
          type="button"
          size="sm"
          disabled={disabled || text.trim().length === 0}
          onClick={doSend}
        >
          {t("group.composer.send")}
        </Button>
      </div>
    </div>
  );
}

export { GroupComposer };
