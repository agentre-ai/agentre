import * as React from "react";

import { monacoLanguageForPath } from "@/lib/file-preview/monaco-language";
import type {
  MonacoCodeEditor,
  MonacoNS,
} from "@/lib/file-preview/monaco-loader";

import {
  resolveMonacoTheme,
  useMonaco,
  useMonacoThemeSync,
} from "./use-monaco";

export type CodePreviewProps = {
  /** 文件正文（UTF-8）。内容变化时原地更新模型，不重建编辑器（保留滚动位置）。 */
  value: string;
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

// 只读代码 / 文本渲染器（Monaco）。语言按扩展名推断，未知扩展名纯文本。
export function CodePreview({
  value,
  path,
  language,
  monaco,
  ariaLabel,
  className,
}: CodePreviewProps) {
  const containerRef = React.useRef<HTMLDivElement>(null);
  const editorRef = React.useRef<MonacoCodeEditor | null>(null);
  const ns = useMonaco(monaco);
  const lang = language ?? (path ? monacoLanguageForPath(path) : "plaintext");
  useMonacoThemeSync(ns);

  // 编辑器只建一次：ns / 语言 / 无障碍标签变化才重建；value 刷新走下方
  // [value] effect 原地 setValue，避免轮次结束重读时重建编辑器丢滚动位置。
  React.useEffect(() => {
    if (!ns || !containerRef.current) return;
    const editor = ns.editor.create(containerRef.current, {
      value,
      language: lang,
      readOnly: true,
      automaticLayout: true,
      minimap: { enabled: false },
      scrollBeyondLastLine: false,
      fontSize: 13,
      tabSize: 2,
      ariaLabel,
      theme: resolveMonacoTheme(),
    });
    editorRef.current = editor;
    return () => {
      editor.dispose();
      editorRef.current = null;
    };
    // value 故意不进 deps：见上方注释，由 [value] effect 维护。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ns, lang, ariaLabel]);

  React.useEffect(() => {
    editorRef.current?.setValue(value);
  }, [value]);

  return <div ref={containerRef} className={className} />;
}
