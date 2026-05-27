import * as React from "react";
import {
  Briefcase,
  ChevronDown,
  MoreVertical,
  Plus,
  Search,
  Settings,
  X,
} from "lucide-react";

import { useSessionStatusOverlay } from "@/hooks/use-live-session-status";
import { reloadProjectTreeCache } from "@/hooks/use-project-tree";
import {
  reasonToDisplayStatus,
  reasonToPillText,
} from "@/lib/attention-display";
import { relativeTime } from "@/lib/relative-time";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import {
  computeAttention,
  type AttentionReason,
} from "@/stores/attention-store";
import { useNewChatContextStore } from "@/stores/new-chat-context-store";
import { useChatAgentsStore } from "@/stores/chat-agents-store";
import { useProjectSessionsStore } from "@/stores/project-sessions-store";
import { useSessionReadStore } from "@/stores/session-read-store";

import type { AgentSession } from "./agent-list";
import { useChatTabsStore } from "@/stores/chat-tabs-store";
import { AgentAvatar } from "./primitives";
import { ProjectNewDialog } from "./project-new-dialog";
import { ProjectSettingsDrawer } from "./project-settings-drawer";
import { ResizableSidebar } from "./resizable-sidebar";
import { SessionGroup } from "./session-group";
import { SessionsPopover } from "./sessions-popover";
import { ProjectGet, ProjectListSessions } from "../../../wailsjs/go/app/App";
import type { chat_svc, app } from "../../../wailsjs/go/models";
import type { AgentColor, AgentStatus } from "./types";

type ProjectTreeNode = app.ProjectTreeNode;
type ProjectSessionItem = app.ProjectSessionItem;
type ChatAgentItem = chat_svc.ChatAgentItem;
type ProjectMemberItem = app.ProjectMemberItem & {
  agentName?: string;
  avatarColor?: string;
  avatarIcon?: string;
  avatarDataUrl?: string;
};

// 项目页激活会话的最低描述 —— 选中已有会话或新建。
type ProjectSelection =
  | { kind: "session"; projectID: number; session: ProjectSessionItem }
  | {
      kind: "new";
      projectID: number;
      agentID: number;
    };

// projectSessionToAgentSession —— 把 ProjectSessionItem + attention reason
// 投影成 SessionGroup 需要的 AgentSession。
function projectSessionToAgentSession(
  s: ProjectSessionItem,
  reason: AttentionReason | "selected" | null,
): AgentSession {
  const title = s.title || "(未命名会话)";
  const attentionReason = reason === "selected" ? null : reason;
  const status = reasonToDisplayStatus(
    attentionReason,
    (s.agentStatus as AgentStatus) || "idle",
  );
  const trailing =
    status === "running"
      ? "running"
      : status === "waiting"
        ? (reasonToPillText(attentionReason) ?? "")
        : status === "error"
          ? "error"
          : relativeTime(s.lastMessageAt);
  return {
    id: String(s.id),
    status,
    title,
    trailingLabel: trailing,
    ...(reason !== null ? { attentionRank: reason } : {}),
  };
}

function ProjectsPage() {
  const [tree, setTree] = React.useState<ProjectTreeNode[]>([]);
  // sessions 与 reload 都从 project-sessions-store 拿。这样 ChatPanelHost
  // 在新建会话 / turn 落定时调一次 reloadSidebarSources(), 本页 sidebar
  // 立刻看到变化, 不必等本组件 re-mount。
  const sessions = useProjectSessionsStore((s) => s.sessionsByProject);
  const reloadProjectSessions = useProjectSessionsStore((s) => s.reload);
  const [loading, setLoading] = React.useState(true);
  const [loadError, setLoadError] = React.useState<string | null>(null);
  const [filter, setFilter] = React.useState("");
  const [selection, setSelection] = React.useState<ProjectSelection | null>(
    null,
  );
  const [newDialogOpen, setNewDialogOpen] = React.useState(false);
  const [newDialogParent, setNewDialogParent] = React.useState(0);
  const [settingsProjectID, setSettingsProjectID] = React.useState(0);
  const refresh = React.useCallback(async () => {
    setLoading(true);
    setLoadError(null);
    try {
      const t = (await reloadProjectTreeCache()) ?? [];
      setTree(t);
      // tree 已经写回 use-project-tree 缓存, store 内部 ensureProjectTreeLoaded
      // 会复用这份缓存, 不再额外发 ProjectListTree。
      await reloadProjectSessions();
      // 展开偏好交给 SessionGroup 的 localStorage 持久化（key=project:ID），
      // SessionGroup 在没有持久化条目时使用 defaultExpanded=true 表现为"默认全开"，
      // 用户折叠过的项目则保持折叠态。
    } catch (err) {
      setLoadError(String(err));
    } finally {
      setLoading(false);
    }
  }, [reloadProjectSessions]);

  React.useEffect(() => {
    void refresh();
  }, [refresh]);

  const totalCount = React.useMemo(() => {
    let n = 0;
    const walk = (ns: ProjectTreeNode[]) => {
      for (const node of ns) {
        n += 1;
        if (node.children) walk(node.children);
      }
    };
    walk(tree);
    return n;
  }, [tree]);

  // 把命令面板的「新会话」上下文桥接到 project-page：
  //   1) selection 切到某项目时，把 {projectID, projectName} 写到 new-chat-context-store
  //      → 命令面板 NewChatSource 能据此分组成员/非成员 + 显示项目 chip
  //   2) 注册 newSelectionHandler，命令面板 onSelect 项目内成员 agent 时
  //      由 project-page 把 selection 翻成 {kind:"new", projectID, agent}
  //
  // unmount cleanup 用 ownership check：只清自己写的那条，避免被切到其它页面后
  // 又被这里的 cleanup 覆盖。
  const currentProjectID =
    selection?.kind === "session"
      ? selection.projectID
      : selection?.kind === "new"
        ? selection.projectID
        : 0;
  const projectByID = React.useMemo(() => {
    const map = new Map<number, app.ProjectItem>();
    const walk = (ns: ProjectTreeNode[]) => {
      for (const n of ns) {
        if (n.project) map.set(n.project.id, n.project);
        if (n.children) walk(n.children);
      }
    };
    walk(tree);
    return map;
  }, [tree]);

  React.useEffect(() => {
    const store = useNewChatContextStore.getState();
    if (currentProjectID > 0) {
      const project = projectByID.get(currentProjectID);
      store.setContext({
        projectID: currentProjectID,
        projectName: project?.name ?? "",
      });
    } else {
      store.setContext(null);
    }
    return () => {
      const cur = useNewChatContextStore.getState().projectContext;
      if (cur && cur.projectID === currentProjectID) {
        useNewChatContextStore.getState().setContext(null);
      }
    };
  }, [currentProjectID, projectByID]);

  // Tab 化后：openSession / openNewSession 把右栏交给 AppLayout 的 ChatPanelHost。
  // 本页内 selection 状态仍存,负责 sidebar 高亮 / sortAttention 锚点。
  const openSession = useChatTabsStore((s) => s.openSession);
  const openSessionInNewTab = useChatTabsStore((s) => s.openSessionInNewTab);
  const openNewSession = useChatTabsStore((s) => s.openNewSession);

  // selectOnTab: 写本地 selection + 同步推到 chat-tabs-store。
  // opts.newTab = true 时(cmd/ctrl+click)对 session 强制新开 tab,不复用 preview。
  const selectOnTab = React.useCallback(
    (next: ProjectSelection | null, opts?: { newTab?: boolean }) => {
      setSelection(next);
      if (next?.kind === "session") {
        if (opts?.newTab) openSessionInNewTab(next.session.id);
        else openSession(next.session.id);
      } else if (next?.kind === "new")
        openNewSession(next.projectID, next.agentID, "");
    },
    [openSession, openSessionInNewTab, openNewSession],
  );

  React.useEffect(() => {
    const handler = (projectID: number, agent: ChatAgentItem) => {
      selectOnTab({ kind: "new", projectID, agentID: agent.id });
    };
    useNewChatContextStore.getState().setNewSelectionHandler(handler);
    return () => {
      const cur = useNewChatContextStore.getState().newSelectionHandler;
      if (cur === handler) {
        useNewChatContextStore.getState().setNewSelectionHandler(null);
      }
    };
  }, []);

  const openCreateDialog = (parentID = 0) => {
    setNewDialogParent(parentID);
    setNewDialogOpen(true);
  };

  const refreshProjectData = React.useCallback(() => {
    void refresh();
  }, [refresh]);

  return (
    <>
      {/* ── 左侧 ProjectList ── */}
      <ResizableSidebar persistenceKey="projects" ariaLabel="项目列表">
        <div className="flex flex-col gap-2 border-b border-border px-4 py-3">
          <div className="flex items-center gap-2">
            <span className="text-sm font-semibold">Projects</span>
            <span className="font-mono text-2xs text-muted-foreground">
              {totalCount}
            </span>
            <div className="min-w-0 flex-1" />
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="size-7"
              aria-label="新建项目"
              title="新建项目"
              onClick={() => openCreateDialog(0)}
            >
              <Plus className="size-4" aria-hidden="true" />
            </Button>
          </div>
          <div className="relative">
            <Search
              className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground"
              aria-hidden="true"
            />
            <Input
              aria-label="搜索项目 / 会话"
              placeholder="搜索项目 / 会话"
              className="h-[30px] bg-input-bg pl-8 pr-7 text-xs"
              value={filter}
              onChange={(event) => setFilter(event.target.value)}
            />
            {filter ? (
              <button
                type="button"
                aria-label="清空筛选"
                className="absolute right-1.5 top-1/2 inline-flex size-5 -translate-y-1/2 cursor-pointer items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                onClick={() => setFilter("")}
              >
                <X className="size-3" aria-hidden="true" />
              </button>
            ) : null}
          </div>
        </div>

        <div className="min-h-0 flex-1 overflow-auto px-2 py-3">
          {loading ? (
            <div className="px-2 py-6 text-center text-2xs text-muted-foreground">
              加载中…
            </div>
          ) : loadError ? (
            <div className="px-2 py-6 text-center text-2xs text-destructive">
              加载失败：{loadError}
            </div>
          ) : tree.length === 0 ? (
            <EmptyTree onCreate={() => openCreateDialog(0)} />
          ) : (
            <div className="flex flex-col gap-1">
              {tree.map((node) => (
                <ProjectCard
                  key={node.project?.id ?? 0}
                  node={node}
                  depth={0}
                  filter={filter}
                  sessions={sessions}
                  selection={selection}
                  onSelect={selectOnTab}
                  onOpenSettings={(id) => setSettingsProjectID(id)}
                  onAddSubProject={(id) => openCreateDialog(id)}
                />
              ))}
            </div>
          )}
        </div>
      </ResizableSidebar>

      {/* 右栏由 AppLayout 的 ChatPanelHost 统一渲染 —— 本页只管左侧树。 */}

      {/* ── 弹窗群 ── */}
      <ProjectNewDialog
        open={newDialogOpen}
        onOpenChange={setNewDialogOpen}
        tree={tree}
        initialParentID={newDialogParent}
        onCreated={refreshProjectData}
      />
      <ProjectSettingsDrawer
        projectID={settingsProjectID}
        onClose={() => setSettingsProjectID(0)}
        onChanged={refreshProjectData}
        onDeleted={() => {
          setSettingsProjectID(0);
          refreshProjectData();
        }}
      />
    </>
  );
}

type EmptyTreeProps = { onCreate: () => void };

function EmptyTree({ onCreate }: EmptyTreeProps) {
  return (
    <div className="mx-2 flex flex-col items-start gap-3 rounded-md border border-dashed border-border bg-card/40 px-3 py-4 text-xs">
      <div className="flex items-center gap-2 text-foreground">
        <Briefcase className="size-4 text-primary-text" aria-hidden="true" />
        <span className="font-semibold">还没有项目</span>
      </div>
      <p className="text-muted-foreground">
        把一个本地代码仓库登记为项目，就可以让多个 Agent 在这里协作。
      </p>
      <Button
        type="button"
        size="sm"
        variant="outline"
        className="h-7 gap-1 px-2 text-2xs"
        onClick={onCreate}
      >
        <Plus className="size-3.5" aria-hidden="true" />
        新建项目
      </Button>
    </div>
  );
}

// 节点匹配筛选 —— 名字 / session title 任一命中即匹配（向上同步父项目）。
function nodeMatches(
  node: ProjectTreeNode,
  filter: string,
  sessions: Map<number, ProjectSessionItem[]>,
): boolean {
  const trimmed = filter.trim().toLowerCase();
  if (!trimmed) return true;
  const name = (node.project?.name ?? "").toLowerCase();
  if (name.includes(trimmed)) return true;
  for (const s of sessions.get(node.project?.id ?? 0) ?? []) {
    if ((s.title ?? "").toLowerCase().includes(trimmed)) return true;
  }
  for (const child of node.children ?? []) {
    if (nodeMatches(child, filter, sessions)) return true;
  }
  return false;
}

type ProjectCardProps = {
  node: ProjectTreeNode;
  depth: number;
  filter: string;
  sessions: Map<number, ProjectSessionItem[]>;
  selection: ProjectSelection | null;
  onSelect: (sel: ProjectSelection | null, opts?: { newTab?: boolean }) => void;
  onOpenSettings: (id: number) => void;
  onAddSubProject: (parentID: number) => void;
};

function ProjectCard({
  node,
  depth,
  filter,
  sessions,
  selection,
  onSelect,
  onOpenSettings,
  onAddSubProject,
}: ProjectCardProps) {
  // 同 ChatPage 的 overlay 来源：服务端 lastReadAt 为持久化真值；
  // useChatSession.reload 写到 store 后，用 withReadOverlay 做本次渲染的乐观覆盖。
  // hook 必须在所有 early return 之前调用，遵守 rules-of-hooks。
  const readOverrides = useSessionReadStore((s) => s.overrides);
  // 项目级会话列表来自 ProjectListSessions 一次性快照；turn 进行中后端推
  // session_status 事件不会回写到这块快照。在这里叠 live patch 让 sidebar 行
  // 实时跟着翻 waiting / running —— 没有活跃 stream 时返回原引用，零成本。
  const rawOwnSessions = sessions.get(node.project?.id ?? -1) ?? [];
  const ownSessions = useSessionStatusOverlay(rawOwnSessions);

  const project = node.project;
  // ── 为何这里直接调用 computeAttention 而不走 useSessionAttentionList ──
  //
  // project-page 的会话列表来自独立 RPC ProjectListSessions(pid)，返回
  // app.ProjectSessionItem 快照，**不经过** session-meta-store。
  // useSessionAttentionList 内部从 session-meta-store.metas 读取数据，
  // 凡是未在 meta-store 注册的 session 都会被 `if (!meta) continue` 跳过，
  // 导致 project-page 的 attention 全部消失。
  //
  // computeAttention 是同一个纯函数（attention 判断逻辑统一），在此内联
  // 调用只是跳过 meta-store 这一层，直接使用 ProjectSessionItem 的字段。
  //
  // 后续若把 project-page 的数据流也接入 meta-store（在收到
  // ProjectListSessions 返回时批量写入），届时可改用 useSessionAttentionList。
  // 这里可删掉内联 computeAttention。
  //
  // 注：既有 commit e4bb8b4 消息表述偏宽（"改用 useSessionAttentionList"），
  // 实际走的是 computeAttention 内联，原因见上。
  const isSelectedInThisProject =
    !!project &&
    selection?.kind === "session" &&
    selection.projectID === project.id;
  const selectedSessionIdForRank =
    isSelectedInThisProject && selection?.kind === "session"
      ? selection.session.id
      : undefined;

  const attentionAgentSessions: AgentSession[] = React.useMemo(() => {
    const rows: { session: ProjectSessionItem; reason: AttentionReason }[] = [];
    const seen = new Set<number>();
    for (const s of ownSessions) {
      const lastReadAt = Math.max(
        s.lastReadAt ?? 0,
        readOverrides.get(s.id) ?? 0,
      );
      const reason = computeAttention({
        // Wails boundary: ProjectSessionItem.agentStatus is string; cast to AgentStatus.
        agentStatus: (s.agentStatus as AgentStatus) || "idle",
        needsAttention: s.needsAttention ?? false,
        lastMessageAt: s.lastMessageAt,
        lastReadAt,
      });
      if (reason !== null) {
        seen.add(s.id);
        rows.push({ session: s, reason });
      }
    }
    rows.sort((a, b) => b.session.lastMessageAt - a.session.lastMessageAt);
    const out: AgentSession[] = rows.map(({ session, reason }) =>
      projectSessionToAgentSession(session, reason),
    );
    // selected 锚点：当前打开的会话即使不在 attention 池，也钉到末尾
    if (selectedSessionIdForRank && !seen.has(selectedSessionIdForRank)) {
      const target = ownSessions.find((s) => s.id === selectedSessionIdForRank);
      if (target) out.push(projectSessionToAgentSession(target, "selected"));
    }
    return out;
  }, [ownSessions, readOverrides, selectedSessionIdForRank]);

  if (!project) return null;
  if (!nodeMatches(node, filter, sessions)) return null;
  const children = node.children ?? [];
  // 活跃会话 = running / waiting，spec L0QoU 头部的绿点 + 数字。
  const activeCount = ownSessions.filter(
    (s) => s.agentStatus === "running" || s.agentStatus === "waiting",
  ).length;

  // 常规列表：所有会话按 lastMessageAt 倒序，前 5 条入侧栏；超 5 走 popover。
  const sortedAll = ownSessions
    .slice()
    .sort((a, b) => b.lastMessageAt - a.lastMessageAt);
  const top5 = sortedAll.slice(0, 5);
  const top5AgentSessions: AgentSession[] = top5.map((s) => {
    const lastReadAt = Math.max(
      s.lastReadAt ?? 0,
      readOverrides.get(s.id) ?? 0,
    );
    const reason = computeAttention({
      // Wails boundary: ProjectSessionItem.agentStatus is string; cast to AgentStatus.
      agentStatus: (s.agentStatus as AgentStatus) || "idle",
      needsAttention: s.needsAttention ?? false,
      lastMessageAt: s.lastMessageAt,
      lastReadAt,
    });
    return projectSessionToAgentSession(s, reason);
  });

  const selectedSessionIdStr = isSelectedInThisProject
    ? String(selection.session.id)
    : undefined;

  const handleSessionSelect = (sid: string, opts?: { newTab?: boolean }) => {
    const num = Number(sid);
    const s = ownSessions.find((x) => x.id === num);
    if (s)
      onSelect({ kind: "session", projectID: project.id, session: s }, opts);
  };

  // depth > 0 时仅靠 pl-1 (4px) 给一点缩进；层级表达全部由 renderHeader 里的
  // UPPERCASE mono section label + 字号 / 颜色差异承担 —— 不再用左竖线 / 大缩进。
  const isSub = depth > 0;
  const isDeep = depth >= 2;

  return (
    <div className={cn(isSub && "pl-1")}>
      <SessionGroup
        persistenceKey={`project:${project.id}`}
        defaultExpanded
        sessions={top5AgentSessions}
        totalSessions={ownSessions.length > 5 ? ownSessions.length : undefined}
        selectedSessionId={selectedSessionIdStr}
        onSessionSelect={handleSessionSelect}
        attentionSessions={attentionAgentSessions}
        attentionAriaLabel={`${project.name} 待处理会话`}
        emptyLabel={children.length === 0 ? "暂无会话" : null}
        renderSessionsPopover={(close) => (
          <SessionsPopover
            header={{
              name: project.name,
              avatarColor: project.color,
              avatarIcon: project.icon || "folder",
              activeCount,
            }}
            loader={async ({ offset, limit }) => {
              // 后端 ProjectListSessions 暂不分页；客户端切片即可。
              const all = (await ProjectListSessions(project.id)) ?? [];
              const slice = all.slice(offset, offset + limit);
              return {
                sessions: slice.map((s) => ({
                  id: s.id,
                  title: s.title,
                  status: s.agentStatus,
                  lastMessageAt: s.lastMessageAt,
                })),
                total: all.length,
                hasMore: offset + limit < all.length,
              };
            }}
            onClose={close}
            onSelectSession={(sid, opts) => {
              const s = ownSessions.find((x) => x.id === sid);
              if (s) {
                onSelect(
                  {
                    kind: "session",
                    projectID: project.id,
                    session: s,
                  },
                  opts,
                );
              }
            }}
          />
        )}
        renderAfterSessions={
          children.length > 0 ? (
            <div className="mt-1 flex flex-col gap-0.5">
              {children.map((child) => (
                <ProjectCard
                  key={child.project?.id ?? 0}
                  node={child}
                  depth={depth + 1}
                  filter={filter}
                  sessions={sessions}
                  selection={selection}
                  onSelect={onSelect}
                  onOpenSettings={onOpenSettings}
                  onAddSubProject={onAddSubProject}
                />
              ))}
            </div>
          ) : undefined
        }
        renderHeader={({ expanded, toggle }) => (
          <div
            className={cn(
              "group/proj flex items-center gap-1.5 rounded-md text-xs hover:bg-sidebar-active-bg",
              isSub ? "px-1.5 py-1" : "px-2 py-1.5",
            )}
          >
            <button
              type="button"
              className="flex min-w-0 flex-1 items-center gap-1.5 outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
              onClick={toggle}
              aria-expanded={expanded}
            >
              <ChevronDown
                className={cn(
                  "text-muted-foreground transition-transform duration-150 ease-out motion-reduce:transition-none",
                  isSub ? "size-3" : "size-3.5",
                  !expanded && "-rotate-90",
                )}
                aria-hidden="true"
              />
              <AgentAvatar
                name={project.name}
                initials={project.name.charAt(0)}
                color={(project.color as AgentColor) || "agent-1"}
                avatarIcon={project.icon || "folder"}
                size="sm"
                className={cn(
                  isDeep
                    ? "size-3.5 rounded-sm"
                    : isSub
                      ? "size-4 rounded-sm"
                      : "size-6 rounded-md",
                )}
              />
              <span
                className={cn(
                  "min-w-0 flex-1 truncate text-left",
                  isDeep
                    ? "font-mono text-[9px] font-medium uppercase tracking-widest text-subtle-foreground"
                    : isSub
                      ? "font-mono text-2xs font-semibold uppercase tracking-wider text-muted-foreground"
                      : "text-[15px] font-semibold",
                )}
              >
                {project.name}
              </span>
              {activeCount > 0 ? (
                <span
                  className="inline-flex items-center gap-1 font-mono text-2xs text-status-running"
                  title={`${activeCount} 个活跃会话`}
                >
                  <span
                    aria-hidden="true"
                    className="inline-block size-1.5 rounded-full bg-status-running"
                  />
                  {activeCount}
                </span>
              ) : null}
            </button>
            <NewSessionMenu
              project={project}
              onPick={(agentID) =>
                onSelect({
                  kind: "new",
                  projectID: project.id,
                  agentID,
                })
              }
            />
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <button
                  type="button"
                  aria-label={`${project.name} 更多操作`}
                  className="inline-flex size-5 shrink-0 cursor-pointer items-center justify-center rounded text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-foreground group-hover/proj:opacity-100 focus:opacity-100 focus-visible:opacity-100"
                >
                  <MoreVertical className="size-3" aria-hidden="true" />
                </button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onSelect={() => onOpenSettings(project.id)}>
                  <Settings className="size-3.5" aria-hidden="true" />
                  项目设置
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => onAddSubProject(project.id)}>
                  <Plus className="size-3.5" aria-hidden="true" />
                  新建子项目
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        )}
      />
    </div>
  );
}

// NewSessionMenu —— ProjectCard 头部的"+"按钮 + 成员 agent 列表下拉。
// 弹出时 lazy 拉取该项目成员，渲染可选 agent 行（直属 + 继承），点击 → onPick。
type NewSessionMenuProps = {
  project: app.ProjectItem;
  onPick: (agentID: number) => void;
};

type MemberMenuLoadState =
  | { status: "idle"; projectID: number; members: ProjectMemberItem[] }
  | { status: "loading"; projectID: number; members: ProjectMemberItem[] }
  | { status: "loaded"; projectID: number; members: ProjectMemberItem[] }
  | {
      status: "error";
      projectID: number;
      members: ProjectMemberItem[];
      error: string;
    };

function NewSessionMenu({ project, onPick }: NewSessionMenuProps) {
  const [open, setOpen] = React.useState(false);
  const [loadState, setLoadState] = React.useState<MemberMenuLoadState>({
    status: "idle",
    projectID: 0,
    members: [],
  });
  const agents = useChatAgentsStore((s) => s.agents);
  const agentByID = React.useMemo(
    () => new Map(agents.map((a) => [a.id, a])),
    [agents],
  );
  const handleOpenChange = React.useCallback(
    (nextOpen: boolean) => {
      setOpen(nextOpen);
      if (nextOpen) {
        setLoadState({
          status: "loading",
          projectID: project.id,
          members: [],
        });
      }
    },
    [project.id],
  );

  React.useEffect(() => {
    if (!open) return;
    let cancelled = false;
    void ProjectGet(project.id)
      .then((detail) => {
        if (cancelled) return;
        setLoadState({
          status: "loaded",
          projectID: project.id,
          members: [
            ...((detail.directMembers ?? []) as ProjectMemberItem[]),
            ...((detail.inheritedMembers ?? []) as ProjectMemberItem[]),
          ],
        });
      })
      .catch((err) => {
        if (!cancelled) {
          setLoadState({
            status: "error",
            projectID: project.id,
            members: [],
            error: String(err),
          });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [open, project.id]);

  const activeLoadState =
    loadState.projectID === project.id
      ? loadState
      : { status: "loading" as const, projectID: project.id, members: [] };
  const members = activeLoadState.members;

  return (
    <DropdownMenu open={open} onOpenChange={handleOpenChange}>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          aria-label={`${project.name} 新建会话`}
          title="新建会话"
          className="inline-flex size-5 shrink-0 cursor-pointer items-center justify-center rounded text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-foreground group-hover/proj:opacity-100 focus:opacity-100 focus-visible:opacity-100"
        >
          <Plus className="size-3" aria-hidden="true" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-[220px]">
        <div className="px-2 py-1.5 font-mono text-2xs uppercase tracking-wider text-subtle-foreground">
          选一个 Agent
        </div>
        {activeLoadState.status === "loading" ? (
          <div className="px-3 py-3 text-2xs text-muted-foreground">
            加载成员中…
          </div>
        ) : activeLoadState.status === "error" ? (
          <div className="px-3 py-3 text-2xs text-destructive">
            加载成员失败：{activeLoadState.error}
          </div>
        ) : members.length === 0 ? (
          <div className="px-3 py-3 text-2xs text-muted-foreground">
            还没添加成员，去项目设置加几个先。
          </div>
        ) : (
          members.map((m) => {
            const agent = agentByID.get(m.agentID);
            const name = m.agentName || agent?.name || `Agent #${m.agentID}`;
            const avatarColor =
              (m.avatarColor as AgentColor) ||
              (agent?.avatarColor as AgentColor) ||
              "agent-1";
            const avatarIcon = m.avatarIcon || agent?.avatarIcon || undefined;
            const avatarDataUrl =
              m.avatarDataUrl || agent?.avatarDataUrl || undefined;
            return (
              <DropdownMenuItem
                key={`${m.inherited ? "i" : "d"}-${m.agentID}`}
                onSelect={() => {
                  onPick(m.agentID);
                  setOpen(false);
                }}
              >
                <AgentAvatar
                  name={name}
                  initials={name.charAt(0)}
                  color={avatarColor}
                  avatarIcon={avatarIcon}
                  avatarDataUrl={avatarDataUrl}
                  size="sm"
                />
                <span className="min-w-0 flex-1 truncate">{name}</span>
                {m.inherited ? (
                  <span className="rounded-sm bg-secondary px-1.5 py-0.5 text-2xs text-muted-foreground">
                    继承
                  </span>
                ) : null}
              </DropdownMenuItem>
            );
          })
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export { ProjectsPage };
