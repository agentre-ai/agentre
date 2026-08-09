import { FileCode, FileImage, FileText, type LucideIcon } from "lucide-react";

import type { PreviewKind } from "../chat-context-sidebar/previewable";
import { previewKind } from "../chat-context-sidebar/previewable";

/** 会话级 relPath 的文件名部分。 */
export function basename(path: string): string {
  const parts = path.split(/[\\/]/);
  return parts[parts.length - 1] ?? path;
}

/** 会话级 relPath 的目录前缀（含末尾分隔符）；根目录下的文件返回空串。 */
export function dirname(path: string): string {
  const i = path.lastIndexOf("/");
  return i > 0 ? path.slice(0, i + 1) : "";
}

/** 预览面板与标签条共用的类型图标表。 */
export const PREVIEW_KIND_ICON: Record<PreviewKind, LucideIcon> = {
  markdown: FileText,
  code: FileCode,
  image: FileImage,
};

/** 取一条路径在图标表里的档；不在 allowlist 内的路径回落到代码图标。 */
export function previewIconKind(path: string): PreviewKind {
  return previewKind(path) ?? "code";
}
