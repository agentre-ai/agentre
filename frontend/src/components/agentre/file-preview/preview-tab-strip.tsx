import * as React from "react";
import { useTranslation } from "react-i18next";

import {
  ArrowRightFromLine,
  ChevronDown,
  Pin,
  PinOff,
  X,
  XCircle,
} from "lucide-react";

import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";
import {
  useChatSidebarStore,
  type FilePreviewTab,
} from "@/stores/chat-sidebar-store";

import { basename, PREVIEW_KIND_ICON, previewIconKind } from "./file-meta";

type Props = {
  sessionId: number;
};

/**
 * PreviewTabStrip 是预览面板 header 之上的标签条（spec「多标签预览」）：只在标签数
 * ≥ 2 时渲染——单文件路径与改造前完全一致，不为多标签能力给单文件用户加一行 chrome。
 *
 * 语义与应用内既有的会话标签条（chat-tabs/tab-strip.tsx）保持一致：临时标签斜体、
 * 活动标签背景提亮加顶部主色条、双击转常驻、右键菜单固定 / 关闭 / 关闭其他 / 关闭
 * 右侧。标签等比收缩到下限后横向滚动，右端是常驻的溢出菜单（列出全部标签、可直接
 * 切换），活动标签始终自动滚入可见区。
 */
export function PreviewTabStrip({ sessionId }: Props) {
  const { t } = useTranslation();
  const entry = useChatSidebarStore((s) => s.previewTabsBySession[sessionId]);
  const activatePreviewTab = useChatSidebarStore((s) => s.activatePreviewTab);
  const promoteActivePreviewTab = useChatSidebarStore(
    (s) => s.promoteActivePreviewTab,
  );
  const togglePreviewTabPin = useChatSidebarStore((s) => s.togglePreviewTabPin);
  const closePreviewTab = useChatSidebarStore((s) => s.closePreviewTab);
  const closeOtherPreviewTabs = useChatSidebarStore(
    (s) => s.closeOtherPreviewTabs,
  );
  const closePreviewTabsToRight = useChatSidebarStore(
    (s) => s.closePreviewTabsToRight,
  );

  const tabs = entry?.tabs ?? [];
  if (tabs.length < 2) return null;

  return (
    <div
      role="tablist"
      aria-label={t("chatContext.filePreview.tabsAria")}
      className="flex h-[34px] shrink-0 items-stretch overflow-hidden border-b border-border bg-muted"
    >
      <div className="scrollbar-none flex h-full min-h-0 min-w-0 flex-1 items-stretch overflow-x-auto overflow-y-hidden">
        {tabs.map((tab, idx) => (
          <PreviewTab
            key={tab.path}
            tab={tab}
            active={tab.path === entry?.activePath}
            isLast={idx === tabs.length - 1}
            onActivate={() => activatePreviewTab(sessionId, tab.path)}
            onDoublePromote={() => {
              activatePreviewTab(sessionId, tab.path);
              promoteActivePreviewTab(sessionId);
            }}
            onTogglePin={() => togglePreviewTabPin(sessionId, tab.path)}
            onClose={() => closePreviewTab(sessionId, tab.path)}
            onCloseOthers={() => closeOtherPreviewTabs(sessionId, tab.path)}
            onCloseToRight={() => closePreviewTabsToRight(sessionId, tab.path)}
          />
        ))}
      </div>
      <div className="flex h-full shrink-0 items-center border-l border-border px-1">
        <PreviewTabOverflowMenu
          tabs={tabs}
          activePath={entry?.activePath ?? null}
          onSelect={(path) => activatePreviewTab(sessionId, path)}
        />
      </div>
    </div>
  );
}

function PreviewTab({
  tab,
  active,
  isLast,
  onActivate,
  onDoublePromote,
  onTogglePin,
  onClose,
  onCloseOthers,
  onCloseToRight,
}: {
  tab: FilePreviewTab;
  active: boolean;
  isLast: boolean;
  onActivate: () => void;
  onDoublePromote: () => void;
  onTogglePin: () => void;
  onClose: () => void;
  onCloseOthers: () => void;
  onCloseToRight: () => void;
}) {
  const { t } = useTranslation();
  const Icon = PREVIEW_KIND_ICON[previewIconKind(tab.path)];
  const ref = React.useRef<HTMLSpanElement>(null);

  // 活动标签始终滚入可见区：标签条超出宽度后靠横向滚动而不是继续压缩。
  React.useEffect(() => {
    if (!active) return;
    ref.current?.scrollIntoView?.({ block: "nearest", inline: "nearest" });
  }, [active]);

  return (
    <ContextMenu>
      <ContextMenuTrigger
        ref={ref}
        className="inline-flex h-full min-w-0 flex-shrink"
      >
        <div
          role="tab"
          aria-selected={active}
          data-active={active}
          data-preview={tab.isPreview}
          title={tab.path}
          onClick={onActivate}
          onDoubleClick={onDoublePromote}
          className={cn(
            "relative flex h-full min-w-[96px] max-w-[150px] flex-shrink items-center gap-1.5 border-r border-border pl-2.5 pr-1.5 text-[11px]",
            active
              ? "bg-background text-foreground"
              : "text-muted-foreground hover:bg-card",
          )}
        >
          {active ? (
            <span className="absolute left-0 top-0 h-[2px] w-full bg-primary" />
          ) : null}
          <Icon className="size-3.5 shrink-0" aria-hidden="true" />
          {tab.isPinned ? (
            <Pin
              data-testid="preview-tab-pin-icon"
              className="size-2.5 shrink-0 rotate-[30deg] text-primary"
              aria-hidden="true"
            />
          ) : null}
          <span
            className={cn(
              "min-w-0 flex-1 truncate font-mono",
              active && "font-semibold",
              // 临时标签用斜体区分（颜色已被主色与状态色占满）。
              tab.isPreview && "italic",
            )}
          >
            {basename(tab.path)}
          </span>
          <button
            type="button"
            aria-label={t("chatTabs.actions.closeTab")}
            className="inline-flex size-4 shrink-0 items-center justify-center rounded-sm text-muted-foreground hover:bg-accent hover:text-foreground"
            onClick={(e) => {
              e.stopPropagation();
              onClose();
            }}
          >
            <X className="size-2.5" aria-hidden="true" />
          </button>
        </div>
      </ContextMenuTrigger>
      <ContextMenuContent>
        <ContextMenuItem onSelect={onTogglePin}>
          {tab.isPinned ? <PinOff /> : <Pin />}
          <span>
            {tab.isPinned
              ? t("chatTabs.actions.unpin")
              : t("chatTabs.actions.pin")}
          </span>
        </ContextMenuItem>
        <ContextMenuSeparator />
        <ContextMenuItem onSelect={onClose}>
          <X />
          <span>{t("common.close")}</span>
        </ContextMenuItem>
        <ContextMenuItem onSelect={onCloseOthers}>
          <XCircle />
          <span>{t("chatTabs.actions.closeOthers")}</span>
        </ContextMenuItem>
        <ContextMenuItem onSelect={onCloseToRight} disabled={isLast}>
          <ArrowRightFromLine />
          <span>{t("chatTabs.actions.closeRight")}</span>
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  );
}

/** 右端常驻的溢出菜单：列出全部标签（含被滚出可见区的），点一条直接切过去。 */
function PreviewTabOverflowMenu({
  tabs,
  activePath,
  onSelect,
}: {
  tabs: FilePreviewTab[];
  activePath: string | null;
  onSelect: (path: string) => void;
}) {
  const { t } = useTranslation();
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          aria-label={t("chatTabs.overflow.openMenu")}
          className="inline-flex size-6 items-center justify-center rounded-md hover:bg-accent"
        >
          <ChevronDown className="size-3.5" aria-hidden="true" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" sideOffset={4} className="w-72 p-0">
        <div className="flex flex-col py-1">
          {tabs.map((tab) => {
            const Icon = PREVIEW_KIND_ICON[previewIconKind(tab.path)];
            return (
              <DropdownMenuItem
                key={tab.path}
                data-active={tab.path === activePath}
                onSelect={() => onSelect(tab.path)}
                className={cn(
                  "h-7 gap-2 rounded-none px-3 text-xs",
                  tab.path === activePath && "bg-sidebar-active-bg",
                )}
              >
                <Icon
                  className="size-3.5 shrink-0 text-muted-foreground"
                  aria-hidden="true"
                />
                <span
                  className={cn(
                    "shrink-0 truncate font-mono",
                    tab.isPreview && "italic",
                  )}
                >
                  {basename(tab.path)}
                </span>
                <span
                  dir="rtl"
                  className="min-w-0 flex-1 truncate text-left font-mono text-[10px] text-muted-foreground"
                >
                  {tab.path}
                </span>
              </DropdownMenuItem>
            );
          })}
        </div>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
