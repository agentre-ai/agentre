import * as React from "react";
import { useTranslation } from "react-i18next";

import { cn } from "@/lib/utils";

import { tokenToCssColor } from "../../session-avatar";
import type { MentionItem, MentionMenuState } from "./types";

// MentionPopover 视觉层:光标上方 fixed 弹层,agent / project 分组渲染。
// 键盘选中在 useMentionMenu 里处理;本组件只渲染 + 鼠标 hover/点击。
// items 已按 agents-first / projects-last 排好序,selectedIndex 是扁平下标。
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

  if (!state.open || !state.anchorRect || state.items.length === 0) return null;

  const style: React.CSSProperties = {
    position: "fixed",
    left: state.anchorRect.left,
    bottom: window.innerHeight - state.anchorRect.top + 4,
    zIndex: 50,
  };

  return (
    <div
      role="listbox"
      aria-label={t("mentions.aria")}
      style={style}
      className="min-w-[14rem] max-w-[20rem] rounded-md border border-border bg-popover p-1 text-popover-foreground shadow-md"
    >
      {state.items.map((item, idx) => {
        const active = idx === state.selectedIndex;
        const prevKind = idx > 0 ? state.items[idx - 1].kind : null;
        const showHeader = item.kind !== prevKind;
        const css = tokenToCssColor(item.color) ?? "var(--muted-foreground)";
        return (
          <React.Fragment key={`${item.kind}-${item.refId}`}>
            {showHeader ? (
              <div className="px-2 pt-1.5 pb-0.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
                {item.kind === "agent"
                  ? t("mentions.group.agents")
                  : t("mentions.group.projects")}
              </div>
            ) : null}
            <button
              type="button"
              role="option"
              aria-selected={active}
              onMouseEnter={() => onHover(idx)}
              onMouseDown={(e) => {
                e.preventDefault();
                onPick(item);
              }}
              className={cn(
                "flex w-full cursor-pointer items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs",
                active
                  ? "bg-accent text-accent-foreground"
                  : "text-foreground hover:bg-accent/60",
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
