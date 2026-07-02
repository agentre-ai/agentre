// cycleTabId —— 计算 Ctrl+Tab / Ctrl+Shift+Tab 循环切换的目标 Tab id。
// orderedIds 是 TabStrip 的可见顺序（钉住 + 普通 + 预览）；activeId 是当前激活 Tab。
// delta > 0 取下一个、delta < 0 取上一个，两端环绕（wrap-around）。
// 边界：空列表返回 null；activeId 不在列表里时，向前落到首个、向后落到末个。
export function cycleTabId(
  orderedIds: string[],
  activeId: string | null,
  delta: number,
): string | null {
  const n = orderedIds.length;
  if (n === 0) return null;
  const idx = activeId === null ? -1 : orderedIds.indexOf(activeId);
  if (idx < 0) return delta >= 0 ? orderedIds[0]! : orderedIds[n - 1]!;
  const next = (((idx + delta) % n) + n) % n;
  return orderedIds[next]!;
}
