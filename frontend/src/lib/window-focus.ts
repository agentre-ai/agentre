// 跟踪应用窗口是否处于前台/聚焦。初值按当前真实状态计算（避免启动即后台时误报聚焦），之后事件驱动更新。
function compute(): boolean {
  if (typeof document === "undefined") return true;
  const visible = document.visibilityState !== "hidden";
  const hasFocus =
    typeof document.hasFocus === "function" ? document.hasFocus() : true;
  return hasFocus && visible;
}

let focused = typeof window !== "undefined" ? compute() : true;

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
