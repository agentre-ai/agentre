import * as React from "react";

import { cn } from "@/lib/utils";

const CURSOR_GAP = 4;
const VIEWPORT_MARGIN = 8;
const MAX_HEIGHT = 288;
const MIN_HEIGHT = 96;

type SuggestionPopoverProps = {
  open: boolean;
  anchorRect: { left: number; top: number; bottom: number } | null;
  selectedIndex: number;
  itemCount: number;
  ariaLabel: string;
  testId?: string;
  className?: string;
  footer?: (activeRef: React.Ref<HTMLButtonElement>) => React.ReactNode;
  children: (activeRef: React.Ref<HTMLButtonElement>) => React.ReactNode;
};

// SuggestionPopover 统一输入框候选菜单的视口行为：以光标为锚点向上展开，
// 最多显示约 9 行，空间不足时缩短并内部滚动，键盘高亮项始终回到可视区。
export function SuggestionPopover({
  open,
  anchorRect,
  selectedIndex,
  itemCount,
  ariaLabel,
  testId,
  className,
  footer,
  children,
}: SuggestionPopoverProps): React.ReactElement | null {
  const activeRef = React.useRef<HTMLButtonElement>(null);

  React.useEffect(() => {
    activeRef.current?.scrollIntoView({ block: "nearest" });
  }, [selectedIndex, itemCount, open]);

  if (!open || !anchorRect || itemCount === 0) return null;

  const roomAbove = anchorRect.top - CURSOR_GAP - VIEWPORT_MARGIN;
  const style: React.CSSProperties = {
    position: "fixed",
    left: anchorRect.left,
    bottom: window.innerHeight - anchorRect.top + CURSOR_GAP,
    maxHeight: Math.max(MIN_HEIGHT, Math.min(MAX_HEIGHT, roomAbove)),
    zIndex: 50,
  };

  const popoverClassName = cn(
    "min-w-[14rem] max-w-[20rem] overflow-y-auto overscroll-contain rounded-md border border-border bg-popover p-1 text-popover-foreground shadow-md",
    className,
  );

  if (!footer) {
    return (
      <div
        data-testid={testId}
        role="listbox"
        aria-label={ariaLabel}
        style={style}
        className={popoverClassName}
      >
        {children(activeRef)}
      </div>
    );
  }

  return (
    <div data-testid={testId} style={style} className={popoverClassName}>
      <div role="listbox" aria-label={ariaLabel}>
        {children(activeRef)}
      </div>
      {footer(activeRef)}
    </div>
  );
}
