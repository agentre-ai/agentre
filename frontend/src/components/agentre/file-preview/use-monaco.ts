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

// 让已创建的 Monaco 编辑器跟随应用明暗切换（terminal-panel 同款 MutationObserver
// 先例）：编辑器建好后主题是全局的，app 在 documentElement 上翻转 .dark 时调
// ns.editor.setTheme 重涂；不重建编辑器，保留滚动位置。单测 fake monaco 需带
// editor.setTheme（见各组件测试的 fake 定义）。
export function useMonacoThemeSync(ns: MonacoNS | null): void {
  React.useEffect(() => {
    if (!ns || typeof document === "undefined") return;
    const apply = () => ns.editor.setTheme(resolveMonacoTheme());
    // 挂载时先按当前主题涂一次（与 terminal 同一理由：App 的 layout effect 可能
    // 晚于本组件首次 mount，observer 看不到那次 class 翻转）。
    apply();
    const observer = new MutationObserver(apply);
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class"],
    });
    return () => observer.disconnect();
  }, [ns]);
}
