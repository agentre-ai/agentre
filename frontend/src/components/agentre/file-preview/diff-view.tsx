import * as React from "react";

import { monacoLanguageForPath } from "@/lib/file-preview/monaco-language";
import type { MonacoNS } from "@/lib/file-preview/monaco-loader";

import { resolveMonacoTheme, useMonaco } from "./use-monaco";

export type DiffPreviewProps = {
  /** 左列 = HEAD 版本（未跟踪文件传空串 → 全部新增）。 */
  original: string;
  /** 右列 = 工作区版本。 */
  modified: string;
  /** 文件路径：用于按扩展名推断语言（未知 → plaintext）。 */
  path?: string;
  /** 显式 Monaco 语言 id，优先于 path 推断。 */
  language?: string;
  /** 注入 seam：happy-dom 单测传入 fake monaco（见 monaco-loader 注释）。 */
  monaco?: MonacoNS | null;
  /** Monaco 无障碍标签（读屏）。 */
  ariaLabel?: string;
  className?: string;
};

// 只读 diff 渲染器（Monaco diff editor）：左 = HEAD 版本、右 = 工作区。
// 与 CodePreview 复用同一份动态加载的 Monaco 命名空间单例（见 monaco-loader）。
// 内容（original/modified）变化时重建 diff editor —— 预览刷新频率低（轮次结束 /
// 切文件），重建可接受，且 setModel 模型所有权交由 diff editor 在 dispose 时释放。
export function DiffPreview({
  original,
  modified,
  path,
  language,
  monaco,
  ariaLabel,
  className,
}: DiffPreviewProps) {
  const containerRef = React.useRef<HTMLDivElement>(null);
  const ns = useMonaco(monaco);
  const lang = language ?? (path ? monacoLanguageForPath(path) : "plaintext");

  React.useEffect(() => {
    if (!ns || !containerRef.current) return;
    const diff = ns.editor.createDiffEditor(containerRef.current, {
      readOnly: true,
      automaticLayout: true,
      minimap: { enabled: false },
      scrollBeyondLastLine: false,
      fontSize: 13,
      renderSideBySide: true,
      ariaLabel,
      theme: resolveMonacoTheme(),
    });
    diff.setModel({
      original: ns.editor.createModel(original, lang),
      modified: ns.editor.createModel(modified, lang),
    });
    return () => {
      // dispose 时一并释放它持有的 original/modified 模型，无手动泄漏。
      diff.dispose();
    };
  }, [ns, lang, original, modified, ariaLabel]);

  return <div ref={containerRef} className={className} />;
}
