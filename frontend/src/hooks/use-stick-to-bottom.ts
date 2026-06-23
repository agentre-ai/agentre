import * as React from "react";

// 贴底容差：与单聊 chat-panel 的 32px 保持一致。
export const STICK_TO_BOTTOM_TOLERANCE_PX = 32;

// isAtBottom：纯函数，判断滚动容器是否在底部容差内。抽出来便于单测。
export function isAtBottom(
  metrics: { scrollTop: number; scrollHeight: number; clientHeight: number },
  tolerance: number = STICK_TO_BOTTOM_TOLERANCE_PX,
): boolean {
  return (
    metrics.scrollHeight - metrics.scrollTop - metrics.clientHeight <= tolerance
  );
}

// useStickToBottom：容器 ref 驱动的「自动跟随滚动」。与虚拟化无关。
// - onScroll：挂到滚动容器，实时更新 atBottom；
// - dep 变化(消息追加)时若先前贴底则滚到底；用户上滚后不抢滚；
// - scrollToBottom：供「回到底部」按钮主动拉回。
export function useStickToBottom(dep: unknown) {
  const ref = React.useRef<HTMLElement | null>(null);
  const atBottomRef = React.useRef(true);
  const [atBottom, setAtBottom] = React.useState(true);

  const onScroll = React.useCallback(() => {
    const el = ref.current;
    if (!el) return;
    const next = isAtBottom(el);
    atBottomRef.current = next;
    setAtBottom(next);
  }, []);

  const scrollToBottom = React.useCallback(() => {
    const el = ref.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
    atBottomRef.current = true;
    setAtBottom(true);
  }, []);

  React.useLayoutEffect(() => {
    if (!atBottomRef.current) return;
    const el = ref.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [dep]);

  return { ref, atBottom, scrollToBottom, onScroll };
}
