// 跟踪应用窗口是否处于前台/聚焦。事件驱动（不依赖 document.hasFocus()，便于测试）。
let focused = true;

function set(v: boolean): void {
  focused = v;
}

if (typeof window !== "undefined") {
  window.addEventListener("focus", () => set(true));
  window.addEventListener("blur", () => set(false));
  if (typeof document !== "undefined") {
    document.addEventListener("visibilitychange", () => {
      set(document.visibilityState !== "hidden");
    });
  }
}

// isWindowFocused 当前窗口是否聚焦且可见。
export function isWindowFocused(): boolean {
  return focused;
}
