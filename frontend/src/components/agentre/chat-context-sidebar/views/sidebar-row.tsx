import {
  Ellipsis,
  File as FileGeneric,
  FileCode,
  FileImage,
  FileText,
} from "lucide-react";
import * as React from "react";
import { useTranslation } from "react-i18next";

import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { copyTextWithToast } from "@/lib/clipboard-toast";
import { cn } from "@/lib/utils";
import { useChatSidebarStore } from "@/stores/chat-sidebar-store";

import type { PreviewSourceMode } from "@/stores/chat-sidebar-store";

import { previewKind, resolvePreviewRelPath, toRelPath } from "../previewable";

import {
  CONTEXT_MENU_PARTS,
  DROPDOWN_MENU_PARTS,
  RowMenu,
  type RowMenuModel,
} from "./row-menu";
import { indentStyle } from "./tree-indent";
import { openTarget, useOpenFile, useRevealFile } from "./use-open-file";

type Props = {
  sessionId: number;
  /** 会话工作目录；空串表示会话没有工作目录（本机能力随之消失）。 */
  cwd: string;
  remote: boolean;
  /** 行来自哪个模式：决定预览标签的首视图，也决定有没有「跳到对应轮次」。 */
  sourceMode: PreviewSourceMode;
  kind: "file" | "dir";
  /** 行的路径：「变动」模式可能是工具调用给的绝对路径，其余模式相对 cwd。 */
  path: string;
  /** 显示名（basename 或目录名）。 */
  name: string;
  depth: number;
  title?: string;
  /** 仅「变动」模式的文件行：跳到该文件最后被改动的轮次。 */
  onJumpToTurn?: () => void;
  /** 仅目录行。 */
  expanded?: boolean;
  onToggle?: () => void;
  /** 主按钮的可访问名；目录行用「展开 / 收起 X」，文件行留空取文本内容。 */
  ariaLabel?: string;
  /** 名称之前的固定宽列：树模式是展开箭头 + 类型图标，Git 模式是状态字母。 */
  lead: React.ReactNode;
  /** 名称之后、⋯ 之前的模式特有信息（diff 角标、目录后缀…）。 */
  trailing?: React.ReactNode;
  testId: string;
  className?: string;
  /** 额外的 data-* 属性（各模式的既有断言锚点）。 */
  rowData?: Record<string, string | undefined>;
  /**
   * 是否给这一行菜单与 ⋯ 槽位。默认给；只有「变动」模式的目录行不给 —— spec
   * 「右键菜单」列举的是「三种模式的文件行、目录模式的目录行」，而变动模式的
   * 目录行是从工具调用路径派生出的结构分组，本身没有可靠的磁盘路径（工具给的
   * 可能是绝对路径），「在文件管理器中显示 / 复制绝对路径」会指向不存在的位置。
   */
  withMenu?: boolean;
};

/**
 * SidebarRow 是「变动 / 目录 / Git」三个模式唯一的行渲染实现。
 *
 * 收敛到一处是刻意的：三种模式此前各写一遍行，点击语义因此长期分叉（变动跳轮次、
 * Git 打开文件、目录不可点）。现在单击语义只有一份——可预览的文件行单击 = 开临时
 * 预览标签、双击 = 转常驻，目录行单击 = 展开收起，不可预览的文件行不响应单击也不
 * 出 hover 高亮（spec「行的形态与交互」）。行右端是恒占 24px、hover 或键盘聚焦才
 * 显形的 ⋯ 槽位，它与右键打开同一份 RowMenu。
 */
export function SidebarRow({
  sessionId,
  cwd,
  remote,
  sourceMode,
  kind,
  path,
  name,
  depth,
  title,
  onJumpToTurn,
  expanded = false,
  onToggle,
  ariaLabel,
  lead,
  trailing,
  testId,
  className,
  rowData,
  withMenu = true,
}: Props) {
  const { t } = useTranslation();
  const openPreview = useChatSidebarStore((s) => s.openPreview);
  const openPreviewInNewTab = useChatSidebarStore((s) => s.openPreviewInNewTab);
  const activePreviewPath = useChatSidebarStore(
    (s) => s.previewTabsBySession[sessionId]?.activePath,
  );
  const openFile = useOpenFile(cwd);
  const revealFile = useRevealFile(cwd);

  // 可预览性与「路径 → 会话级 relPath」的判定只有 previewable.ts 一处；null 表示
  // 扩展名不在 allowlist 内，或绝对路径解析后落在 cwd 之外。
  const previewPath = kind === "file" ? resolvePreviewRelPath(path, cwd) : null;
  // 复制相对路径：换算不出相对路径（越出 cwd）时退回行本身的路径，菜单项不消失。
  const relPath = toRelPath(path, cwd) ?? path;
  // 远端会话拿到的是对端路径，本机命令不能碰；无 cwd 时也拼不出绝对路径。
  const absPath = remote || cwd === "" ? null : openTarget(path, cwd);

  const interactive = kind === "dir" || previewPath !== null;
  const active = previewPath !== null && previewPath === activePreviewPath;

  const copy = React.useCallback(
    (text: string) => {
      void copyTextWithToast(text, { successTitle: t("common.copied") });
    },
    [t],
  );

  const model: RowMenuModel = {
    kind,
    previewPath,
    absPath,
    relPath,
    name,
    expanded,
    onToggle: () => onToggle?.(),
    onPreview: () => {
      if (previewPath !== null) openPreview(sessionId, previewPath, sourceMode);
    },
    onPreviewInNewTab: () => {
      if (previewPath !== null) {
        openPreviewInNewTab(sessionId, previewPath, sourceMode);
      }
    },
    onJumpToTurn: onJumpToTurn ?? null,
    onOpenWith: () => openFile(path),
    onReveal: () => revealFile(path),
    onCopy: copy,
  };

  const body = (
    <>
      {lead}
      <span className="min-w-0 shrink truncate font-mono">{name}</span>
      {trailing}
    </>
  );
  const bodyClassName =
    "flex min-w-0 flex-1 items-center gap-1.5 py-1.5 text-left";

  const row = (
    <div
      {...rowData}
      data-testid={testId}
      data-name={name}
      title={title}
      style={indentStyle(depth)}
      className={cn(
        "group/row flex items-center gap-1.5 rounded-md pr-1 pl-2 text-xs text-muted-foreground transition-colors",
        interactive && "hover:bg-muted/50",
        active && "bg-accent text-accent-foreground",
        className,
      )}
    >
      {interactive ? (
        <button
          type="button"
          aria-label={ariaLabel}
          aria-expanded={kind === "dir" ? expanded : undefined}
          className={bodyClassName}
          onClick={() =>
            kind === "dir" ? model.onToggle() : model.onPreview()
          }
          // 双击 = 转常驻标签，与预览标签条的双击语义一致；目录行没有第二种打开
          // 方式，双击就是连续两次展开收起，交给 onClick 自然处理。
          onDoubleClick={
            kind === "file" ? () => model.onPreviewInNewTab() : undefined
          }
        >
          {body}
        </button>
      ) : (
        <div className={bodyClassName}>{body}</div>
      )}
      {withMenu ? (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button
              type="button"
              aria-label={t("chatContext.row.menu")}
              // 槽位恒占 24px、只切换可见性：条件渲染会让整行文字在 hover 时左右
              // 跳（spec「行的形态与交互」）。
              className="flex size-6 shrink-0 items-center justify-center rounded-md text-muted-foreground opacity-0 transition-opacity group-hover/row:opacity-100 hover:text-foreground focus-visible:opacity-100 data-[state=open]:opacity-100"
            >
              <Ellipsis className="size-3.5" aria-hidden="true" />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <RowMenu model={model} parts={DROPDOWN_MENU_PARTS} />
          </DropdownMenuContent>
        </DropdownMenu>
      ) : null}
    </div>
  );

  if (!withMenu) return row;
  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>{row}</ContextMenuTrigger>
      <ContextMenuContent>
        <RowMenu model={model} parts={CONTEXT_MENU_PARTS} />
      </ContextMenuContent>
    </ContextMenu>
  );
}

/**
 * FileTypeIcon 按扩展名分四类：markdown / 图片 / 代码文本 / 不在 allowlist 内的
 * 通用文件。分类判定复用 previewKind —— 图标与「能不能预览」永远说同一件事。
 */
export function FileTypeIcon({ path }: { path: string }) {
  const kind = previewKind(path);
  const Icon =
    kind === "markdown"
      ? FileText
      : kind === "image"
        ? FileImage
        : kind === "code"
          ? FileCode
          : FileGeneric;
  return (
    <Icon
      data-testid="row-file-icon"
      data-file-kind={kind ?? "other"}
      className="size-3.5 shrink-0"
      aria-hidden="true"
    />
  );
}
