import * as React from "react";

import { loadMonaco, type MonacoNS } from "@/lib/file-preview/monaco-loader";

// 解析 Monaco 命名空间：注入（单测 / 特殊宿主）优先，否则异步经 loadMonaco()
// 动态加载。加载失败时静默返回 null，容器保持空白，由调用方的错误态兜底。
export function useMonaco(injected?: MonacoNS | null): MonacoNS | null {
  const [ns, setNs] = React.useState<MonacoNS | null>(injected ?? null);

  React.useEffect(() => {
    if (injected) {
      setNs(injected);
      return;
    }
    let cancelled = false;
    loadMonaco()
      .then((m) => {
        if (!cancelled) setNs(m);
      })
      .catch(() => {
        /* 加载失败：保持 null，容器留空。 */
      });
    return () => {
      cancelled = true;
    };
  }, [injected]);

  return ns;
}

// Monaco 主题跟随应用 .dark class（与 xterm 的 terminal-theme 同一判定）。
export function resolveMonacoTheme(): "vs-dark" | "vs" {
  return typeof document !== "undefined" &&
    document.documentElement.classList.contains("dark")
    ? "vs-dark"
    : "vs";
}
