import * as React from "react";
import { useTranslation } from "react-i18next";

import { cn } from "@/lib/utils";

import { tokenToCssColor } from "../../session-avatar";
import type { MentionItem, MentionMenuState } from "./types";

// 弹层与光标之间的间距。
const CURSOR_GAP = 4;
// 视口顶部留白,别让弹层贴死边缘。
const VIEWPORT_MARGIN = 8;
// 高度上限 ≈ 9 行。更长的列表靠滚动消化,而不是把自己顶出视口顶部。
const MAX_HEIGHT = 288;
// 光标离视口顶部太近时的下限 ≈ 3 行:宁可少量溢出,也不要退化成一条不可用的缝。
const MIN_HEIGHT = 96;

// MentionPopover 视觉层:光标上方 fixed 弹层,agent / project 分组渲染。
// 键盘选中在 useMentionMenu 里处理;本组件只渲染 + 鼠标 hover/点击。
// items 已按 agents-first / projects-last 排好序,selectedIndex 是扁平下标。
//
// 高度:弹层向上生长,所以上限同时受 MAX_HEIGHT 和「光标上方剩余空间」约束,
// 超出部分滚动。列表限高后,键盘高亮项会跑到滚动区外 —— 故 selectedIndex 变化时
// 把它拉回可视区,分组标题 sticky 以免滚动后丢失「Agents / Projects」上下文。
export function MentionPopover({
  state,
  onPick,
  onHover,
}: {
  state: MentionMenuState;
  onPick: (item: MentionItem) => void;
  onHover: (idx: number) => void;
}): React.ReactElement | null {
  const { t } = useTranslation();
  const activeRef = React.useRef<HTMLButtonElement>(null);

  React.useEffect(() => {
    activeRef.current?.scrollIntoView({ block: "nearest" });
  }, [state.selectedIndex, state.items, state.open]);

  if (!state.open || !state.anchorRect || state.items.length === 0) return null;

  const roomAbove = state.anchorRect.top - CURSOR_GAP - VIEWPORT_MARGIN;
  const style: React.CSSProperties = {
    position: "fixed",
    left: state.anchorRect.left,
    bottom: window.innerHeight - state.anchorRect.top + CURSOR_GAP,
    maxHeight: Math.max(MIN_HEIGHT, Math.min(MAX_HEIGHT, roomAbove)),
    zIndex: 50,
  };

  return (
    <div
      role="listbox"
      aria-label={t("mentions.aria")}
      style={style}
      className="min-w-[14rem] max-w-[20rem] overflow-y-auto overscroll-contain rounded-md border border-border bg-popover p-1 text-popover-foreground shadow-md"
    >
      {state.items.map((item, idx) => {
        const active = idx === state.selectedIndex;
        const prevKind = idx > 0 ? state.items[idx - 1].kind : null;
        const showHeader = item.kind !== prevKind;
        const css = tokenToCssColor(item.color) ?? "var(--muted-foreground)";
        return (
          <React.Fragment key={`${item.kind}-${item.refId}`}>
            {showHeader ? (
              <div className="sticky top-0 z-10 bg-popover px-2 pt-1.5 pb-0.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
                {item.kind === "agent"
                  ? t("mentions.group.agents")
                  : t("mentions.group.projects")}
              </div>
            ) : null}
            <button
              type="button"
              role="option"
              ref={active ? activeRef : undefined}
              aria-selected={active}
              // mousemove 而非 mouseenter —— 键盘翻页让列表在静止的鼠标下滚动时
              // 只有 mouseenter 会触发,那会把选中态抢回鼠标所在行。
              // 指针一动就落到 selectedIndex 上,所以行内不再单独挂 hover 底色:
              // 高亮只有 selectedIndex 一个来源,不会出现键鼠两个高亮态打架。
              onMouseMove={() => onHover(idx)}
              onMouseDown={(e) => {
                e.preventDefault();
                onPick(item);
              }}
              className={cn(
                // scroll-mt 抵掉 sticky 分组标题的高度,免得滚上来的高亮项藏在标题后面。
                "flex w-full scroll-mt-6 cursor-pointer items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs",
                active ? "bg-accent text-accent-foreground" : "text-foreground",
              )}
            >
              <span
                aria-hidden="true"
                className="size-2 shrink-0 rounded-full"
                style={{ backgroundColor: css }}
              />
              <span className="truncate font-medium">{item.label}</span>
              {item.kind === "project" && item.path ? (
                <span className="ml-auto truncate text-muted-foreground">
                  {item.path}
                </span>
              ) : null}
            </button>
          </React.Fragment>
        );
      })}
    </div>
  );
}
