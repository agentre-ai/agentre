import * as React from "react";
import type { TFunction } from "i18next";
import {
  Briefcase,
  ChevronDown,
  MessagesSquare,
  MoreVertical,
  Plus,
  Search,
  Settings,
  TerminalSquare,
  X,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";

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
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
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
import { useRemoteDevices } from "./remote-devices/use-remote-devices";
import { AgentAvatar } from "./primitives";
import { ProjectNewDialog } from "./project-new-dialog";
import { ProjectSettingsDrawer } from "./project-settings-drawer";
import { ResizableSidebar } from "./resizable-sidebar";
import { SessionGroup } from "./session-group";
import { SessionsPopover } from "./sessions-popover";
import * as WailsApp from "../../../wailsjs/go/app/App";
import type { chat_svc, app } from "../../../wailsjs/go/models";
import type { AgentColor, AgentStatus } from "./types";

type ProjectTreeNode = app.ProjectTreeNode;
type ProjectSessionItem = app.ProjectSessionItem;
type ProjectSessionWithProject = ProjectSessionItem & { projectID: number };
type ChatAgentItem = chat_svc.ChatAgentItem;
type ProjectMemberItem = app.ProjectMemberItem & {
  agentName?: string;
  avatarColor?: string;
  avatarIcon?: string;
  avatarDataUrl?: string;
};
type ProjectReorderFn = (req: {
  parentID: number;
  orderedIDs: number[];
}) => Promise<void>;
const ProjectReorder = (
  WailsApp as typeof WailsApp & {
    ProjectReorder: ProjectReorderFn;
  }
).ProjectReorder;

// 项目页激活会话的最低描述 —— 选中已有会话或新建。
type ProjectSelection =
  | { kind: "session"; projectID: number; session: ProjectSessionItem }
  | {
      kind: "new";
      projectID: number;
      agentID: number;
    }
  | {
      kind: "open-terminal";
      projectID: number;
      deviceID: string;
      deviceName?: string;
    };

// projectSessionToAgentSession —— 把 ProjectSessionItem + attention reason
// 投影成 SessionGroup 需要的 AgentSession。
function projectSessionToAgentSession(
  s: ProjectSessionItem,
  reason: AttentionReason | "selected" | null,
  t: TFunction,
): AgentSession {
  const title = s.title || t("projects.session.untitled");
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
    ...(reason === "selected" ? { selected: true } : {}),
    status,
    title,
    trailingLabel: trailing,
    ...(reason !== null ? { attentionRank: reason } : {}),
  };
}

function ProjectsPage() {
  const { t } = useTranslation();
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
  const [reorderError, setReorderError] = React.useState<string | null>(null);
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );
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
  const openTerminal = useChatTabsStore((s) => s.openTerminal);

  // selectOnTab: 写本地 selection + 同步推到 chat-tabs-store。
  // opts.newTab = true 时(cmd/ctrl+click)对 session 强制新开 tab,不复用 preview。
  const selectOnTab = React.useCallback(
    (next: ProjectSelection | null, opts?: { newTab?: boolean }) => {
      setSelection(next);
      if (next?.kind === "open-terminal") {
        openTerminal(next.projectID, next.deviceID, next.deviceName);
        return;
      }
      if (next?.kind === "session") {
        if (opts?.newTab) openSessionInNewTab(next.session.id);
        else openSession(next.session.id);
      } else if (next?.kind === "new")
        openNewSession(next.projectID, next.agentID, "");
    },
    [openSession, openSessionInNewTab, openNewSession, openTerminal],
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

  const dragDisabled = filter.trim().length > 0;
  const handleProjectDragEnd = React.useCallback(
    (event: DragEndEvent) => {
      if (dragDisabled) return;
      const activeID = parseProjectDragID(event.active.id);
      const overID = parseProjectDragID(event.over?.id);
      if (activeID <= 0 || overID <= 0 || activeID === overID) return;
      const activeGroup = findSiblingGroup(tree, activeID);
      const overGroup = findSiblingGroup(tree, overID);
      if (
        !activeGroup ||
        !overGroup ||
        activeGroup.parentID !== overGroup.parentID
      ) {
        return;
      }
      const from = activeGroup.nodes.findIndex(
        (n) => n.project?.id === activeID,
      );
      const to = activeGroup.nodes.findIndex((n) => n.project?.id === overID);
      if (from < 0 || to < 0) return;
      const reordered = moveItem(activeGroup.nodes, from, to);
      const orderedIDs = reordered
        .map((n) => n.project?.id ?? 0)
        .filter((id) => id > 0);
      const previous = tree;
      setReorderError(null);
      setTree(reorderSiblingGroup(tree, activeGroup.parentID, orderedIDs));
      void Promise.resolve()
        .then(() =>
          ProjectReorder({
            parentID: activeGroup.parentID,
            orderedIDs,
          }),
        )
        .then(() => {
          setReorderError(null);
          return refresh();
        })
        .catch((err) => {
          setTree(previous);
          setReorderError(
            t("projects.errors.reorderFailed", { error: String(err) }),
          );
        });
    },
    [dragDisabled, refresh, t, tree],
  );

  return (
    <>
      {/* ── 左侧 ProjectList ── */}
      <ResizableSidebar
        persistenceKey="projects"
        ariaLabel={t("projects.sidebar.aria")}
      >
        <div className="flex flex-col gap-2 border-b border-border px-4 py-3">
          <div className="flex items-center gap-2">
            <span className="text-sm font-semibold">
              {t("projects.sidebar.title")}
            </span>
            <span className="font-mono text-2xs text-muted-foreground">
              {totalCount}
            </span>
            <div className="min-w-0 flex-1" />
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="size-7"
              aria-label={t("projects.actions.newProject")}
              title={t("projects.actions.newProject")}
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
              aria-label={t("projects.search.aria")}
              placeholder={t("projects.search.placeholder")}
              className="h-[30px] bg-input-bg pl-8 pr-7 text-xs"
              value={filter}
              onChange={(event) => setFilter(event.target.value)}
            />
            {filter ? (
              <button
                type="button"
                aria-label={t("projects.search.clear")}
                className="absolute right-1.5 top-1/2 inline-flex size-5 -translate-y-1/2 cursor-pointer items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                onClick={() => setFilter("")}
              >
                <X className="size-3" aria-hidden="true" />
              </button>
            ) : null}
          </div>
          {reorderError ? (
            <div role="status" className="px-0.5 text-2xs text-destructive">
              {reorderError}
            </div>
          ) : null}
        </div>

        <div className="min-h-0 flex-1 overflow-auto px-2 py-3">
          {loading ? (
            <div className="px-2 py-6 text-center text-2xs text-muted-foreground">
              {t("common.loading")}
            </div>
          ) : loadError ? (
            <div className="px-2 py-6 text-center text-2xs text-destructive">
              {t("projects.errors.loadFailed", { error: loadError })}
            </div>
          ) : tree.length === 0 ? (
            <EmptyTree onCreate={() => openCreateDialog(0)} />
          ) : (
            <DndContext sensors={sensors} onDragEnd={handleProjectDragEnd}>
              <div className="flex flex-col gap-1">
                <ProjectSortableList
                  nodes={tree}
                  depth={0}
                  filter={filter}
                  sessions={sessions}
                  selection={selection}
                  onSelect={selectOnTab}
                  onOpenSettings={(id) => setSettingsProjectID(id)}
                  onAddSubProject={(id) => openCreateDialog(id)}
                  dragDisabled={dragDisabled}
                />
              </div>
            </DndContext>
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
  const { t } = useTranslation();
  return (
    <div className="mx-2 flex flex-col items-start gap-3 rounded-md border border-dashed border-border bg-card/40 px-3 py-4 text-xs">
      <div className="flex items-center gap-2 text-foreground">
        <Briefcase className="size-4 text-primary-text" aria-hidden="true" />
        <span className="font-semibold">{t("projects.empty.title")}</span>
      </div>
      <p className="text-muted-foreground">{t("projects.empty.description")}</p>
      <Button
        type="button"
        size="sm"
        variant="outline"
        className="h-7 gap-1 px-2 text-2xs"
        onClick={onCreate}
      >
        <Plus className="size-3.5" aria-hidden="true" />
        {t("projects.actions.newProject")}
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

function collectSubtreeSessions(
  node: ProjectTreeNode,
  sessions: Map<number, ProjectSessionItem[]>,
): ProjectSessionWithProject[] {
  const out: ProjectSessionWithProject[] = [];
  const walk = (n: ProjectTreeNode) => {
    const projectID = n.project?.id ?? 0;
    if (projectID > 0) {
      for (const session of sessions.get(projectID) ?? []) {
        out.push({ ...session, projectID });
      }
    }
    for (const child of n.children ?? []) walk(child);
  };
  walk(node);
  return out;
}

function projectDragID(id: number): string {
  return `project-${id}`;
}

function parseProjectDragID(id: unknown): number {
  const raw = String(id);
  if (!raw.startsWith("project-")) return 0;
  const n = Number(raw.slice("project-".length));
  return Number.isFinite(n) ? n : 0;
}

function moveItem<T>(items: T[], from: number, to: number): T[] {
  const out = items.slice();
  const [item] = out.splice(from, 1);
  out.splice(to, 0, item);
  return out;
}

function findSiblingGroup(
  nodes: ProjectTreeNode[],
  projectID: number,
  parentID = 0,
): { parentID: number; nodes: ProjectTreeNode[] } | null {
  if (nodes.some((n) => n.project?.id === projectID)) {
    return { parentID, nodes };
  }
  for (const n of nodes) {
    const found = findSiblingGroup(
      n.children ?? [],
      projectID,
      n.project?.id ?? 0,
    );
    if (found) return found;
  }
  return null;
}

function reorderSiblingGroup(
  nodes: ProjectTreeNode[],
  parentID: number,
  orderedIDs: number[],
): ProjectTreeNode[] {
  if (parentID === 0) {
    const byID = new Map(nodes.map((n) => [n.project?.id ?? 0, n]));
    return orderedIDs
      .map((id) => byID.get(id))
      .filter(Boolean) as ProjectTreeNode[];
  }
  return nodes.map((n) => {
    if (n.project?.id === parentID) {
      const byID = new Map(
        (n.children ?? []).map((c) => [c.project?.id ?? 0, c]),
      );
      return {
        ...n,
        children: orderedIDs
          .map((id) => byID.get(id))
          .filter(Boolean) as ProjectTreeNode[],
      } as ProjectTreeNode;
    }
    return {
      ...n,
      children: reorderSiblingGroup(n.children ?? [], parentID, orderedIDs),
    } as ProjectTreeNode;
  });
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
  drag?: ProjectDragState;
};

// 拖拽改造（2026-05-28）：整行即拖把手 —— 不再渲染独立 grip 按钮，
// PointerSensor 的 distance:4 已经让点击与拖拽天然分流（<4px 是点击，
// 否则才进入拖拽）。这里只暴露挂在 header 行上的最小集合。
type ProjectDragState = {
  listeners: React.HTMLAttributes<HTMLElement> | undefined;
  setNodeRef: (node: HTMLDivElement | null) => void;
  style: React.CSSProperties;
  isDragging: boolean;
};

type ProjectSortableListProps = Omit<
  ProjectCardProps,
  "node" | "depth" | "drag"
> & {
  nodes: ProjectTreeNode[];
  depth: number;
  dragDisabled: boolean;
};

function ProjectSortableList({
  nodes,
  depth,
  dragDisabled,
  ...cardProps
}: ProjectSortableListProps) {
  return (
    <SortableContext
      items={nodes.map((node) => projectDragID(node.project?.id ?? 0))}
      strategy={verticalListSortingStrategy}
    >
      {nodes.map((node) => (
        <SortableProjectCard
          key={node.project?.id ?? 0}
          node={node}
          depth={depth}
          dragDisabled={dragDisabled}
          {...cardProps}
        />
      ))}
    </SortableContext>
  );
}

type SortableProjectCardProps = ProjectCardProps & {
  dragDisabled: boolean;
};

function SortableProjectCard({
  node,
  dragDisabled,
  ...cardProps
}: SortableProjectCardProps) {
  const { listeners, setNodeRef, transform, transition, isDragging } =
    useSortable({
      id: projectDragID(node.project?.id ?? 0),
      disabled: dragDisabled,
    });
  const style: React.CSSProperties = {
    transform: transform
      ? `translate3d(${transform.x}px, ${transform.y}px, 0) scaleX(${transform.scaleX}) scaleY(${transform.scaleY})`
      : undefined,
    transition,
    opacity: isDragging ? 0.6 : undefined,
  };
  return (
    <ProjectCard
      node={node}
      drag={
        dragDisabled
          ? undefined
          : {
              listeners,
              setNodeRef,
              style,
              isDragging,
            }
      }
      {...cardProps}
    />
  );
}

// SessionsSubGroupHeader —— 父级项目「自己的会话」子组的头部。
// 仅当父级同时有自己的会话和子项目时出现（见 ProjectCard 的 nestOwnSessions），
// 让父级会话能独立于子项目折叠/展开。视觉上与子项目头部同级，但用 messages 图标
// 而非项目头像，明确「这是会话分组、不是子项目」。
function SessionsSubGroupHeader({
  name,
  count,
  expanded,
  toggle,
}: {
  name: string;
  count: number;
  expanded: boolean;
  toggle: () => void;
}) {
  const { t } = useTranslation();
  return (
    <button
      type="button"
      className="flex w-full items-center gap-1.5 rounded-md px-2 py-1 text-xs outline-none hover:bg-sidebar-active-bg focus-visible:ring-[3px] focus-visible:ring-ring/50"
      onClick={toggle}
      aria-expanded={expanded}
      aria-label={t("projects.session.groupToggle", { name })}
    >
      <ChevronDown
        className={cn(
          "size-3 text-muted-foreground transition-transform duration-150 ease-out motion-reduce:transition-none",
          !expanded && "-rotate-90",
        )}
        aria-hidden="true"
      />
      <MessagesSquare
        className="size-3.5 text-muted-foreground"
        aria-hidden="true"
      />
      <span className="font-mono text-2xs font-semibold uppercase tracking-wider text-muted-foreground">
        {t("projects.session.group")}
      </span>
      <span className="font-mono text-2xs text-subtle-foreground">{count}</span>
    </button>
  );
}

function ProjectCard({
  node,
  depth,
  filter,
  sessions,
  selection,
  onSelect,
  onOpenSettings,
  onAddSubProject,
  drag,
}: ProjectCardProps) {
  const { t } = useTranslation();
  // 同 ChatPage 的 overlay 来源：服务端 lastReadAt 为持久化真值；
  // useChatSession.reload 写到 store 后，用 withReadOverlay 做本次渲染的乐观覆盖。
  // hook 必须在所有 early return 之前调用，遵守 rules-of-hooks。
  const readOverrides = useSessionReadStore((s) => s.overrides);
  // 项目级会话列表来自 ProjectListSessions 一次性快照；turn 进行中后端推
  // session_status 事件不会回写到这块快照。在这里叠 live patch 让 sidebar 行
  // 实时跟着翻 waiting / running —— 没有活跃 stream 时返回原引用，零成本。
  const rawOwnSessions = sessions.get(node.project?.id ?? -1) ?? [];
  const ownSessions = useSessionStatusOverlay(rawOwnSessions);
  const rawSubtreeSessions = React.useMemo(
    () => collectSubtreeSessions(node, sessions),
    [node, sessions],
  );
  const subtreeSessions = useSessionStatusOverlay(rawSubtreeSessions);

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
  // 当前激活 tab 的 session id —— 项目页 sidebar 直接订阅 chat-tabs-store，
  // 与对话页 useBuildAttentionSessions 的 selected 锚点语义保持一致。
  // 走本地 selection 时, 任何从外部 (tab strip / chat 页 / 命令面板) 触发的
  // 切换都不会同步, sidebar 锚点会失效。
  const activeSessionId = useChatTabsStore((s) => {
    const id = s.activeTabId;
    if (!id) return 0;
    const tab = s.tabs.find((t) => t.id === id);
    return tab?.meta.kind === "session" ? tab.meta.sessionId : 0;
  });
  const selectedOwnSessionId =
    activeSessionId && ownSessions.some((s) => s.id === activeSessionId)
      ? activeSessionId
      : undefined;
  const selectedSubtreeSession = activeSessionId
    ? subtreeSessions.find((s) => s.id === activeSessionId)
    : undefined;

  const attentionRows = React.useMemo(() => {
    const rows: {
      session: ProjectSessionWithProject;
      reason: AttentionReason | "selected";
    }[] = [];
    const seen = new Set<number>();
    for (const s of subtreeSessions) {
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
    // selected 锚点：当前打开的会话即使不在 attention 池，也钉到末尾
    if (selectedSubtreeSession && !seen.has(selectedSubtreeSession.id)) {
      rows.push({
        session: selectedSubtreeSession,
        reason: "selected",
      });
    }
    return rows;
  }, [readOverrides, selectedSubtreeSession, subtreeSessions]);

  const attentionAgentSessions: AgentSession[] = React.useMemo(
    () =>
      attentionRows
        .filter(({ session }) => session.projectID === (node.project?.id ?? 0))
        .map(({ session, reason }) =>
          projectSessionToAgentSession(session, reason, t),
        ),
    [attentionRows, node.project?.id, t],
  );

  const collapsedAttentionAgentSessions: AgentSession[] = React.useMemo(
    () =>
      attentionRows.map(({ session, reason }) =>
        projectSessionToAgentSession(session, reason, t),
      ),
    [attentionRows, t],
  );

  if (!project) return null;
  if (!nodeMatches(node, filter, sessions)) return null;
  const children = node.children ?? [];
  // 头部活跃数包含当前项目与后代项目的 attention 会话；父项目折叠时也能提示
  // 子项目里有 running / 审批 / 未读入口。
  const activeCount = attentionRows.filter(
    ({ reason }) => reason !== "selected",
  ).length;

  // 常规列表：所有会话按 lastMessageAt 倒序，前 5 条入侧栏；超 5 走 popover。
  // 若当前激活 tab 对应的 session 不在 Top 5 里, 追加到末尾, 让外部切 tab 后
  // 即使是空闲会话也始终可见 —— 与对话页 selected 锚点钉到末尾的行为对齐。
  const sortedAll = ownSessions
    .slice()
    .sort((a, b) => b.lastMessageAt - a.lastMessageAt);
  const top5 = sortedAll.slice(0, 5);
  if (
    selectedOwnSessionId &&
    !top5.some((s) => s.id === selectedOwnSessionId)
  ) {
    const anchor = ownSessions.find((s) => s.id === selectedOwnSessionId);
    if (anchor) top5.push(anchor);
  }
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
    return projectSessionToAgentSession(s, reason, t);
  });

  const selectedSessionIdStr = selectedOwnSessionId
    ? String(selectedOwnSessionId)
    : undefined;

  const handleSessionSelect = (sid: string, opts?: { newTab?: boolean }) => {
    const num = Number(sid);
    const s = subtreeSessions.find((x) => x.id === num);
    if (s)
      onSelect({ kind: "session", projectID: s.projectID, session: s }, opts);
  };

  // depth > 0 时仅靠 pl-1 (4px) 给一点缩进；层级表达全部由 renderHeader 里的
  // UPPERCASE mono section label + 字号 / 颜色差异承担 —— 不再用左竖线 / 大缩进。
  const isSub = depth > 0;
  const isDeep = depth >= 2;

  // 父级「自己的会话」与子项目共用同一个 SessionGroup 的折叠箭头会把两者绑死。
  // 当父级同时有自己的会话和子项目时，把自己的会话拆到一个独立折叠的内层
  // SessionGroup（persistenceKey: project:<id>:sessions），父级标题箭头仍收整卡。
  const nestOwnSessions = ownSessions.length > 0 && children.length > 0;
  const ownSessionsTotal =
    ownSessions.length > 5 ? ownSessions.length : undefined;

  const renderSessionsPopover = (close: () => void) => (
    <SessionsPopover
      header={{
        name: project.name,
        avatarColor: project.color,
        avatarIcon: project.icon || "folder",
        activeCount,
      }}
      loader={async ({ offset, limit }) => {
        // 后端 ProjectListSessions 暂不分页；客户端切片即可。
        const all = (await WailsApp.ProjectListSessions(project.id)) ?? [];
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
        const s = subtreeSessions.find((x) => x.id === sid);
        if (s) {
          onSelect(
            { kind: "session", projectID: s.projectID, session: s },
            opts,
          );
        }
      }}
    />
  );

  const subProjectsList =
    children.length > 0 ? (
      <div className="mt-1 flex flex-col gap-0.5">
        <ProjectSortableList
          nodes={children}
          depth={depth + 1}
          filter={filter}
          sessions={sessions}
          selection={selection}
          onSelect={onSelect}
          onOpenSettings={onOpenSettings}
          onAddSubProject={onAddSubProject}
          dragDisabled={!drag}
        />
      </div>
    ) : null;

  return (
    <div
      ref={drag?.setNodeRef}
      style={drag?.style}
      className={cn(isSub && "pl-1", drag?.isDragging && "relative z-10")}
    >
      <SessionGroup
        persistenceKey={`project:${project.id}`}
        defaultExpanded
        sessions={nestOwnSessions ? [] : top5AgentSessions}
        totalSessions={nestOwnSessions ? undefined : ownSessionsTotal}
        selectedSessionId={selectedSessionIdStr}
        onSessionSelect={handleSessionSelect}
        attentionSessions={nestOwnSessions ? [] : attentionAgentSessions}
        collapsedAttentionSessions={collapsedAttentionAgentSessions}
        attentionAriaLabel={t("projects.session.attentionAria", {
          name: project.name,
        })}
        emptyLabel={children.length === 0 ? t("projects.session.empty") : null}
        renderSessionsPopover={renderSessionsPopover}
        renderAfterSessions={
          children.length > 0 ? (
            <>
              {nestOwnSessions ? (
                <SessionGroup
                  persistenceKey={`project:${project.id}:sessions`}
                  defaultExpanded
                  sessions={top5AgentSessions}
                  totalSessions={ownSessionsTotal}
                  selectedSessionId={selectedSessionIdStr}
                  onSessionSelect={handleSessionSelect}
                  attentionSessions={attentionAgentSessions}
                  attentionAriaLabel={t("projects.session.attentionAria", {
                    name: project.name,
                  })}
                  emptyLabel={null}
                  renderSessionsPopover={renderSessionsPopover}
                  renderHeader={({ expanded, toggle }) => (
                    <SessionsSubGroupHeader
                      name={project.name}
                      count={ownSessions.length}
                      expanded={expanded}
                      toggle={toggle}
                    />
                  )}
                />
              ) : null}
              {subProjectsList}
            </>
          ) : undefined
        }
        renderHeader={({ expanded, toggle }) => (
          <div
            className={cn(
              "group/proj flex items-center gap-1.5 rounded-md text-xs hover:bg-sidebar-active-bg",
              isSub ? "px-1.5 py-1" : "px-2 py-1.5",
              drag && "cursor-grab active:cursor-grabbing",
            )}
            {...(drag?.listeners ?? {})}
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
                  title={t("projects.session.activeCount", {
                    count: activeCount,
                  })}
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
                  aria-label={t("projects.actions.more", {
                    name: project.name,
                  })}
                  className="inline-flex size-5 shrink-0 cursor-pointer items-center justify-center rounded text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-foreground group-hover/proj:opacity-100 focus:opacity-100 focus-visible:opacity-100"
                >
                  <MoreVertical className="size-3" aria-hidden="true" />
                </button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onSelect={() => onOpenSettings(project.id)}>
                  <Settings className="size-3.5" aria-hidden="true" />
                  {t("projectSettings.title")}
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => onAddSubProject(project.id)}>
                  <Plus className="size-3.5" aria-hidden="true" />
                  {t("projects.actions.newSubProject")}
                </DropdownMenuItem>
                <NewTerminalSubMenu
                  projectID={project.id}
                  onPick={(deviceID, deviceName) =>
                    onSelect({
                      kind: "open-terminal",
                      projectID: project.id,
                      deviceID,
                      deviceName,
                    })
                  }
                />
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
  const { t } = useTranslation();
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
    void WailsApp.ProjectGet(project.id)
      .then((detail) => {
        if (cancelled) return;
        const members = [
          ...((detail.directMembers ?? []) as ProjectMemberItem[]),
          ...((detail.inheritedMembers ?? []) as ProjectMemberItem[]),
        ];
        if (members.length === 1) {
          onPick(members[0].agentID);
          setOpen(false);
          return;
        }
        setLoadState({
          status: "loaded",
          projectID: project.id,
          members,
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
  }, [onPick, open, project.id]);

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
          aria-label={t("projects.session.newForProject", {
            name: project.name,
          })}
          title={t("projects.session.new")}
          className="inline-flex size-5 shrink-0 cursor-pointer items-center justify-center rounded text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-foreground group-hover/proj:opacity-100 focus:opacity-100 focus-visible:opacity-100"
        >
          <Plus className="size-3" aria-hidden="true" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="end"
        className="min-w-[220px]"
        // 阻止 Radix 默认把焦点还给 trigger —— 选完 agent 后新 tab 的输入框
        // 已经被 ChatPanelHost 接管，让 Radix 抢回 trigger 会直接抹掉那次 focus。
        onCloseAutoFocus={(e) => e.preventDefault()}
      >
        <div className="px-2 py-1.5 font-mono text-2xs uppercase tracking-wider text-subtle-foreground">
          {t("projects.session.pickAgent")}
        </div>
        {activeLoadState.status === "loading" ? (
          <div className="px-3 py-3 text-2xs text-muted-foreground">
            {t("projects.session.loadingMembers")}
          </div>
        ) : activeLoadState.status === "error" ? (
          <div className="px-3 py-3 text-2xs text-destructive">
            {t("projects.session.loadMembersFailed", {
              error: activeLoadState.error,
            })}
          </div>
        ) : members.length === 0 ? (
          <div className="px-3 py-3 text-2xs text-muted-foreground">
            {t("projects.session.noMembers")}
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
                    {t("projects.session.inherited")}
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

// NewTerminalSubMenu —— ProjectCard「更多操作」里的「新建终端」子菜单。
// 打开时 lazy 加载该项目已配置的 location，结合 device 在线状态决定可选性。
export function NewTerminalSubMenu({
  projectID,
  onPick,
}: {
  projectID: number;
  onPick: (deviceID: string, deviceName?: string) => void;
}) {
  const { t } = useTranslation();
  const { devices } = useRemoteDevices();
  const [configured, setConfigured] = React.useState<Set<string> | null>(null);
  const loadLocations = React.useCallback(() => {
    void WailsApp.ProjectLocationList(projectID).then((rows) =>
      setConfigured(new Set((rows ?? []).map((r) => r.deviceId))),
    );
  }, [projectID]);
  return (
    <DropdownMenuSub
      onOpenChange={(open) => {
        if (open && configured === null) loadLocations();
      }}
    >
      <DropdownMenuSubTrigger>
        <TerminalSquare className="size-3.5" aria-hidden="true" />
        {t("projects.terminal.new")}
      </DropdownMenuSubTrigger>
      <DropdownMenuSubContent>
        <DropdownMenuItem onSelect={() => onPick("", undefined)}>
          {t("projects.terminal.local")}
        </DropdownMenuItem>
        {devices.length > 0 ? <DropdownMenuSeparator /> : null}
        {devices.map((d) => {
          const id = String(d.id);
          const hasPath = configured?.has(id) ?? false;
          const disabled = !d.online || !hasPath;
          return (
            <DropdownMenuItem
              key={id}
              disabled={disabled}
              title={
                !d.online
                  ? t("projects.terminal.deviceOffline")
                  : !hasPath
                    ? t("projects.terminal.pathNotConfigured")
                    : undefined
              }
              onSelect={() => {
                if (!disabled) onPick(id, d.name);
              }}
            >
              {d.name}
              {!d.online
                ? t("projects.terminal.offlineSuffix")
                : !hasPath
                  ? t("projects.terminal.pathMissingSuffix")
                  : ""}
            </DropdownMenuItem>
          );
        })}
      </DropdownMenuSubContent>
    </DropdownMenuSub>
  );
}

export { ProjectsPage };
