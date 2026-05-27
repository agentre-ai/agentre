import * as React from "react";
import { ChevronDown, Pin, Plus } from "lucide-react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

import type { AttentionReason } from "@/stores/attention-store";
import { AgentAvatar, StatusDot } from "./primitives";
import { SessionGroup } from "./session-group";
import type { AgentColor, AgentStatus } from "./types";
import { statusConfig } from "./types";

type AgentPanelSectionProps = React.ComponentProps<"div"> & {
  label: string;
  icon?: "pin";
};

function AgentPanelSection({
  className,
  label,
  icon,
  ...props
}: AgentPanelSectionProps) {
  return (
    <div
      className={cn(
        "flex items-center gap-1 px-2 pb-0.5 font-mono text-2xs font-semibold text-subtle-foreground",
        className,
      )}
      {...props}
    >
      {icon === "pin" ? (
        <Pin className="size-2.5 -rotate-[30deg]" aria-hidden="true" />
      ) : null}
      <span>{label}</span>
    </div>
  );
}

type SessionRowProps = React.ComponentProps<"button"> & {
  selected?: boolean;
  status: AgentStatus;
  title: string;
  trailingLabel: string;
};

function SessionRow({
  "aria-hidden": ariaHidden,
  className,
  disabled,
  selected = false,
  status,
  title,
  trailingLabel,
  ...props
}: SessionRowProps) {
  const config = statusConfig[status];
  const hiddenFromAccessibility = ariaHidden === true || ariaHidden === "true";

  return (
    <button
      type="button"
      aria-hidden={ariaHidden}
      disabled={disabled}
      aria-current={selected ? "true" : undefined}
      className={cn(
        "flex w-full cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs outline-none transition-colors hover:bg-sidebar-active-bg focus-visible:ring-[3px] focus-visible:ring-ring/50",
        selected && "bg-primary-soft text-primary-text",
        className,
      )}
      {...props}
    >
      <StatusDot
        status={status}
        size="xs"
        {...(hiddenFromAccessibility
          ? { "aria-hidden": true, "aria-label": undefined }
          : {})}
      />
      <span
        className={cn(
          "min-w-0 flex-1 truncate",
          selected ? "font-medium text-primary-text" : "text-foreground",
        )}
      >
        {title}
      </span>
      <span
        className={cn(
          "shrink-0 font-mono text-2xs",
          selected ? "text-primary-text" : config.textClassName,
        )}
      >
        {trailingLabel}
      </span>
    </button>
  );
}

type AgentSession = {
  id: string;
  selected?: boolean;
  status: AgentStatus;
  title: string;
  trailingLabel: string;
  // 只有 attentionSessions 数组里的项会有；SessionGroup 在 expanded 态下用它把
  // selected 从 bubble 中过滤掉（让它回到常规列表它本来的位置）；
  // unread / running / error / needs_attention 在 expanded 下也保留在 bubble,
  // 这样按住 ⌘ 时未读会话也能拿到 ⌘N chip。
  attentionRank?: AttentionReason | "selected";
};

type AgentGroupProps = React.ComponentProps<"article"> & {
  activeCount?: number;
  color?: AgentColor;
  expanded?: boolean;
  initials?: string;
  name: string;
  onNewSession?: () => void;
  // 头部 (avatar + 名称区) 被点击：父组件常用这个钩子直接打开「最近活动的会话」，
  // 把"选 agent → 选会话"压成一步。不影响 chevron / + 按钮（这俩 stopPropagation）。
  onHeaderClick?: () => void;
  onSessionSelect?: (sessionId: string, opts?: { newTab?: boolean }) => void;
  persistenceKey?: string;
  pinned?: boolean;
  selectedSessionId?: string;
  sessions?: AgentSession[];
  totalSessions?: number;
  // 折叠态下「冒泡」展示的 attention 行：父组件已按 rank 排序、过滤过非 attention。
  // expanded === false 时永远渲染；expanded === true 时隐藏，避免与下方 5 行列表重复。
  attentionSessions?: AgentSession[];
  // 渲染「查看全部 N 个会话」按钮关联的 popover content；
  // 由父组件提供，避免 agent-list 依赖 chat 业务（依赖反转）。
  renderSessionsPopover?: (close: () => void) => React.ReactNode;
};

function AgentGroup({
  activeCount = 0,
  className,
  color = "agent-1",
  expanded: expandedProp,
  initials,
  name,
  onHeaderClick,
  onNewSession,
  onSessionSelect,
  persistenceKey,
  pinned,
  selectedSessionId,
  sessions = [],
  totalSessions,
  attentionSessions = [],
  renderSessionsPopover,
  ...props
}: AgentGroupProps) {
  const hasActiveSessions = activeCount > 0;
  // expandedProp 仅作为「无持久化时的初始默认值」使用：
  // 之前会在 mount 后根据 expandedProp → true 强制展开，但用户明确希望
  // 选中 agent 时**展开/收起状态保持不变**，所以那个 useEffect 移除。
  // 用户的展开偏好通过 chevron 触发 → 走 readSidebarExpanded 持久化。
  return (
    <SessionGroup
      className={className}
      persistenceKey={persistenceKey}
      defaultExpanded={expandedProp}
      sessions={sessions}
      selectedSessionId={selectedSessionId}
      onSessionSelect={onSessionSelect}
      totalSessions={totalSessions}
      renderSessionsPopover={renderSessionsPopover}
      attentionSessions={attentionSessions}
      attentionAriaLabel={`${name} 待处理会话`}
      {...props}
      renderHeader={({ expanded, toggle }) => (
        <div
          className={cn(
            "flex items-center gap-2 rounded-md px-2 py-1 transition-colors",
            onHeaderClick &&
              "cursor-pointer hover:bg-sidebar-active-bg focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none",
          )}
          role={onHeaderClick ? "button" : undefined}
          tabIndex={onHeaderClick ? 0 : undefined}
          aria-label={onHeaderClick ? `打开 ${name} 最近会话` : undefined}
          onClick={onHeaderClick}
          onKeyDown={
            onHeaderClick
              ? (e) => {
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    onHeaderClick();
                  }
                }
              : undefined
          }
        >
          <AgentAvatar
            name={name}
            initials={initials}
            color={color}
            size="sm"
          />
          <span className="min-w-0 truncate text-sm font-semibold">{name}</span>
          {pinned ? (
            <Pin
              className="size-3 -rotate-[30deg] text-primary-text"
              aria-label="置顶"
            />
          ) : null}
          <span className="min-w-0 flex-1" />
          {hasActiveSessions ? (
            <span
              className="flex shrink-0 items-center"
              title="运行中"
              aria-label={`${name} 有运行中的会话`}
            >
              <StatusDot status="running" size="xs" className="animate-pulse" />
            </span>
          ) : null}
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            aria-label={`新建 ${name} 会话`}
            title={`新建 ${name} 会话`}
            className="text-muted-foreground"
            onClick={(e) => {
              e.stopPropagation();
              onNewSession?.();
            }}
          >
            <Plus data-icon="only" aria-hidden="true" />
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            aria-expanded={expanded}
            aria-label={expanded ? `折叠 ${name}` : `展开 ${name}`}
            title={expanded ? `折叠 ${name}` : `展开 ${name}`}
            className={cn(
              "text-muted-foreground transition-colors",
              expanded && "bg-sidebar-active-bg text-foreground",
            )}
            onClick={(e) => {
              e.stopPropagation();
              toggle();
            }}
          >
            <ChevronDown
              data-icon="only"
              aria-hidden="true"
              className={cn(
                "transition-transform duration-150 ease-out motion-reduce:transition-none",
                expanded && "rotate-180",
              )}
            />
          </Button>
        </div>
      )}
    />
  );
}

export { AgentGroup, AgentPanelSection, SessionRow };
export type { AgentSession };
