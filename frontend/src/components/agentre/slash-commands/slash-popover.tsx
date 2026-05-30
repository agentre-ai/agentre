import * as React from "react";
import { useTranslation } from "react-i18next";

import { cn } from "@/lib/utils";

import type { SlashCommand } from "./registry";
import type { SlashMenuState } from "./use-slash-menu";

// SlashPopover 是 / 命令下拉的视觉层。位置走 fixed,以光标视口坐标为锚点;
// 出现在光标上方(优先 top 方向以避免遮挡正在键入的字符)。
//
// 键盘选中(Up/Down/Enter)在 useSlashMenu 里处理,本组件只负责:
//   - 渲染候选列表
//   - 高亮 selectedIndex 项
//   - 鼠标 hover → 把 selectedIndex 更新到该项
//   - 鼠标点击 → 触发 onPick
//
// 鼠标 hover 与键盘高亮共享同一个 selectedIndex,避免出现两个高亮态打架。
export function SlashPopover({
  state,
  onPick,
  onHover,
}: {
  state: SlashMenuState;
  onPick: (cmd: SlashCommand) => void;
  onHover: (idx: number) => void;
}): React.ReactElement | null {
  const { t } = useTranslation();

  if (!state.open || !state.anchorRect || state.items.length === 0) return null;

  // 弹层放在光标上方;留 4px 间距,避免遮住正在键入的 /xxx 文字。
  const style: React.CSSProperties = {
    position: "fixed",
    left: state.anchorRect.left,
    bottom: window.innerHeight - state.anchorRect.top + 4,
    zIndex: 50,
  };

  return (
    <div
      role="listbox"
      aria-label={t("slashCommands.aria")}
      style={style}
      className="min-w-[14rem] max-w-[20rem] rounded-md border border-border bg-popover p-1 text-popover-foreground shadow-md"
    >
      {state.items.map((cmd, idx) => {
        const active = idx === state.selectedIndex;
        return (
          <button
            key={cmd.name}
            type="button"
            role="option"
            aria-selected={active}
            onMouseEnter={() => onHover(idx)}
            onMouseDown={(e) => {
              // mousedown 而非 click —— 避免编辑器先 blur 再 click,弹层早就关了。
              e.preventDefault();
              onPick(cmd);
            }}
            className={cn(
              "flex w-full cursor-pointer items-center justify-between gap-3 rounded-sm px-2 py-1.5 text-left text-xs",
              active
                ? "bg-accent text-accent-foreground"
                : "text-foreground hover:bg-accent/60",
            )}
          >
            <span className="font-mono font-medium">{cmd.label}</span>
            {cmd.description ? (
              <span className="truncate text-muted-foreground">
                {cmd.description}
              </span>
            ) : null}
          </button>
        );
      })}
    </div>
  );
}
