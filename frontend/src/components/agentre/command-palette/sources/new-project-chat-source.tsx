import * as React from "react";

import { useChatAgents, type ChatAgentItem } from "@/hooks/use-chat-agents";
import { cn } from "@/lib/utils";
import {
  readLastAgentId,
  writeLastAgentId,
} from "@/stores/last-agent-persistence";
import { useCommandPaletteStore } from "@/stores/command-palette-store";
import { useNewChatContextStore } from "@/stores/new-chat-context-store";

import {
  ProjectGet,
  ProjectLocationList,
} from "../../../../../wailsjs/go/app/App";
import { DeviceTag } from "../../device-tag";
import { AgentAvatar } from "../../primitives";
import type { AgentColor } from "../../types";
import { scoreItem } from "../score";
import type { CommandSource, OnSelectCtx } from "../types";

// 命令面板的 "New project chat with <agent>" 命令源 —— 仅在 /projects 路由激活。
// 项目上下文从 useNewChatContextStore 读：
//   - 有 context → 按 members 分组（成员前，非成员置灰）
//   - 无 context（项目页但 tree 没选）→ 列全部 chattable，onSelect 退化到自由会话
export type NewProjectChatItem = {
  key: string;
  // subHeading: 命令面板内的次级分组标签。
  //   - 成员: "在 {projectName} 中新建 chat"
  //   - 非成员: "其它 Agent"
  //   - 无项目 context: undefined(单组)
  subHeading?: string;
  agentId: number;
  agent: ChatAgentItem;
  isMember: boolean;
  // location path for this agent's device on the active project.
  // Empty/undefined when:
  //   - no active project context (item.isMember=true degenerate case)
  //   - agent is local (deviceID === "") — local sessions use project.path natively
  //   - agent is remote but no project_location row exists → user needs to configure
  locationPath?: string;
};

// 平铺：过滤 chattable；有 members 时把成员排前，非成员排后；同组内 pinned 先。
// 没有 members 集时，所有 agent 视为"成员"（项目页未选项目时的退化形态）。
// lastAgentId 若命中"成员组"（或退化模式下的全量组），冒泡到组首；
// 若 lastAgent 不在当前项目的成员里，**不做冒泡** —— "members 优先"是更强的语义。
export function flattenAgents(
  agents: ChatAgentItem[],
  members: Set<number> | null,
  lastAgentId: number | null = null,
  // deviceID → path map for remote agents; localPath for local agents.
  // When undefined, no path preview is rendered (degenerate / non-project context).
  paths?: { localPath?: string; byDeviceID?: Record<string, string> },
  // 拆两个 subHeading group:成员 / 其它 Agent。没有 projectName 时
  // (无 project context)整体不带 subHeading。
  projectName?: string,
): NewProjectChatItem[] {
  const chattable = agents.filter((a) => a.chattable);
  const memberHeading = projectName
    ? `在 ${projectName} 中新建 chat`
    : undefined;
  const otherHeading = projectName ? "其它 Agent" : undefined;

  const resolvePath = (agent: ChatAgentItem): string | undefined => {
    if (!paths) return undefined;
    if (!agent.deviceID) return paths.localPath;
    return paths.byDeviceID?.[agent.deviceID];
  };

  const pinSort = (list: ChatAgentItem[]) => {
    const pinned = list.filter((a) => a.pinned);
    const others = list.filter((a) => !a.pinned);
    return [...pinned, ...others];
  };

  const bubbleLast = (list: ChatAgentItem[]): ChatAgentItem[] => {
    if (lastAgentId == null) return list;
    const idx = list.findIndex((a) => a.id === lastAgentId);
    if (idx <= 0) return list;
    const copy = list.slice();
    const [last] = copy.splice(idx, 1);
    return [last, ...copy];
  };

  if (!members) {
    return bubbleLast(pinSort(chattable)).map((agent) => ({
      key: `new-project-chat-agent-${agent.id}`,
      subHeading: memberHeading,
      agentId: agent.id,
      agent,
      isMember: true,
      locationPath: resolvePath(agent),
    }));
  }

  const m = bubbleLast(pinSort(chattable.filter((a) => members.has(a.id))));
  const n = pinSort(chattable.filter((a) => !members.has(a.id)));
  return [
    ...m.map((agent) => ({
      key: `new-project-chat-agent-${agent.id}`,
      subHeading: memberHeading,
      agentId: agent.id,
      agent,
      isMember: true,
      locationPath: resolvePath(agent),
    })),
    ...n.map((agent) => ({
      key: `new-project-chat-agent-${agent.id}`,
      subHeading: otherHeading,
      agentId: agent.id,
      agent,
      isMember: false,
      locationPath: resolvePath(agent),
    })),
  ];
}

// 拉取项目成员 agentID 集合。每次命令面板打开 / projectID 变化都重新拉，
// 避免项目设置刚添加成员后仍使用旧的空集合。
function useProjectMembers(
  projectID: number,
  enabled: boolean,
): Set<number> | null {
  const [loaded, setLoaded] = React.useState<{
    projectID: number;
    members: Set<number>;
  } | null>(null);

  React.useEffect(() => {
    if (!enabled || projectID <= 0) {
      return;
    }
    let cancelled = false;
    void (async () => {
      try {
        const detail = await ProjectGet(projectID);
        if (cancelled) return;
        const ids = new Set<number>();
        for (const m of detail.directMembers ?? []) ids.add(m.agentID);
        for (const m of detail.inheritedMembers ?? []) ids.add(m.agentID);
        setLoaded({ projectID, members: ids });
      } catch {
        if (!cancelled) setLoaded({ projectID, members: new Set() });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [projectID, enabled]);

  if (!enabled || projectID <= 0 || loaded?.projectID !== projectID)
    return null;
  return loaded.members;
}

function useItems(): { items: NewProjectChatItem[]; loading: boolean } {
  const { agents, loading } = useChatAgents();
  const projectContext = useNewChatContextStore((s) => s.projectContext);
  const paletteOpen = useCommandPaletteStore((s) => s.open);
  const members = useProjectMembers(
    projectContext?.projectID ?? 0,
    paletteOpen,
  );

  // Fetch this project's remote-device paths (deviceID → path map).
  const [byDeviceID, setByDeviceID] = React.useState<Record<string, string>>(
    {},
  );
  React.useEffect(() => {
    const projectID = projectContext?.projectID ?? 0;
    if (projectID <= 0) {
      setByDeviceID({});
      return;
    }
    let cancelled = false;
    void ProjectLocationList(projectID)
      .then((rows) => {
        if (cancelled) return;
        const map: Record<string, string> = {};
        for (const r of rows ?? []) {
          if (r?.deviceId && r?.path) map[r.deviceId] = r.path;
        }
        setByDeviceID(map);
      })
      .catch(() => {
        if (!cancelled) setByDeviceID({});
      });
    return () => {
      cancelled = true;
    };
  }, [projectContext?.projectID]);

  // useState 懒初始化：本次面板打开期间锁定值。
  const [lastAgentId] = React.useState(() => readLastAgentId());

  // Note: ProjectContext in the store only has projectID + projectName, not projectPath.
  // Fetching the project detail just for localPath here would add a second waterfall RPC;
  // we defer that to a follow-up. For now, local agents don't show a path preview —
  // local users already know their project path from the sidebar.
  const items = React.useMemo(
    () =>
      flattenAgents(
        agents,
        projectContext ? members : null,
        lastAgentId,
        projectContext ? { localPath: undefined, byDeviceID } : undefined,
        projectContext?.projectName,
      ),
    [agents, projectContext, members, lastAgentId, byDeviceID],
  );
  return { items, loading };
}

function actionTitle(item: NewProjectChatItem): string {
  return `New project chat with ${item.agent.name}`;
}

const MULTI_TOKEN_SCORE = 25;

function getScore(query: string, item: NewProjectChatItem): number {
  const title = actionTitle(item);
  const direct = scoreItem({
    query,
    title,
    subtitle: item.agent.name,
  });
  if (direct > 0) return direct;

  // Token-based fallback —— 多词查询里每个词都要在 title 里出现（case-insensitive）。
  const tokens = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (tokens.length <= 1) return 0;
  const tl = title.toLowerCase();
  for (const tok of tokens) {
    if (!tl.includes(tok)) return 0;
  }
  return MULTI_TOKEN_SCORE;
}

function renderItem(item: NewProjectChatItem): React.ReactNode {
  return <AgentRow item={item} />;
}

type AgentRowProps = { item: NewProjectChatItem };

function AgentRow({ item }: AgentRowProps) {
  const a = item.agent;
  const offline = !!a.deviceID && a.online === false;
  return (
    <div
      className={cn(
        "flex w-full items-center gap-3",
        (!item.isMember || offline) && "opacity-55",
      )}
    >
      <AgentAvatar
        name={a.name}
        initials={a.name.charAt(0)}
        color={(a.avatarColor as AgentColor) || "agent-1"}
        avatarIcon={a.avatarIcon || undefined}
        avatarDataUrl={a.avatarDataUrl || undefined}
        size="md"
        className="size-7 rounded-md text-xs"
      />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <div
          className="truncate text-sm text-foreground"
          title={`New project chat with ${a.name}`}
        >
          <span className="text-muted-foreground">New project chat with </span>
          <span className="font-medium">{a.name}</span>
        </div>
        {/* Device chip + cwd preview on the subline */}
        <div className="flex items-center gap-2 text-2xs">
          <DeviceTag
            deviceId={a.deviceID ?? ""}
            deviceName={a.deviceName ?? ""}
            online={a.online ?? true}
          />
          {item.locationPath && (
            <span className="truncate font-mono text-muted-foreground">
              {item.locationPath}
            </span>
          )}
        </div>
        {a.chattableHint && item.isMember ? (
          <div className="truncate text-2xs text-muted-foreground">
            {a.chattableHint}
          </div>
        ) : null}
      </div>
      {!item.isMember ? (
        <span className="rounded-sm border border-border bg-muted px-2 py-0.5 text-2xs text-muted-foreground">
          不在该项目
        </span>
      ) : null}
      <kbd
        className={cn(
          "rounded-sm border border-border bg-card px-1.5 py-0.5 font-mono text-2xs font-medium text-muted-foreground",
          "opacity-0 group-data-[selected=true]/cmditem:opacity-100",
        )}
        aria-hidden="true"
      >
        ↵
      </kbd>
    </div>
  );
}

export const PROJECTS_PATH_PREFIX = "/projects";

function isProjectsRoute(pathname: string): boolean {
  return (
    pathname === PROJECTS_PATH_PREFIX ||
    pathname.startsWith(`${PROJECTS_PATH_PREFIX}/`)
  );
}

function dispatchFreeChat(item: NewProjectChatItem, ctx: OnSelectCtx): void {
  ctx.openNewSession(item.agentId);
  try {
    ctx.navigate("/chat");
  } catch (err) {
    console.warn("[command-palette] navigate('/chat') failed", err);
  }
}

function onSelect(item: NewProjectChatItem, ctx: OnSelectCtx): void {
  ctx.close();
  // 记忆最近选过的 agent —— 不论是走项目路径还是兜底自由会话。
  writeLastAgentId(item.agentId);

  // 防御：activeFor 已经限定只在 /projects 激活；万一某天误传 ctx
  // 仍保留这个保护：非 /projects 路由直接走自由会话。
  if (!isProjectsRoute(ctx.pathname)) {
    dispatchFreeChat(item, ctx);
    return;
  }

  const store = useNewChatContextStore.getState();
  const projectContext = store.projectContext;
  const handler = store.newSelectionHandler;

  // 项目页但还没选任何项目（左侧 tree 没选中）→ 没有 project 上下文可挂，
  // 静默退化到自由会话。
  if (!projectContext) {
    dispatchFreeChat(item, ctx);
    return;
  }

  // Remote-device guards: check before member/non-member fallback so we do not
  // silently start a free-chat session for an offline or unconfigured remote agent.
  const a = item.agent;
  const offline = !!a.deviceID && a.online === false;
  if (offline) {
    console.warn(
      `[command-palette] 「${a.name}」所在的设备「${a.deviceName ?? a.deviceID}」当前离线，无法启动会话`,
    );
    return;
  }
  // Remote agent + no configured location → cannot start.
  if (a.deviceID && !item.locationPath) {
    console.warn(
      `[command-palette] 「${a.name}」在该项目的远端机器上没有配置路径，请到「项目设置 · 成员」补齐`,
    );
    return;
  }

  // 项目内成员 + handler 已注册 → 让 project-page 把 selection 翻成 {kind:"new"}。
  if (item.isMember && handler) {
    handler(projectContext.projectID, item.agent);
    return;
  }

  // 非成员（或异常情况：handler 未注册）→ 静默兜底到 /chat 自由会话。
  if (!item.isMember) {
    console.info(
      `[command-palette] 已为「${item.agent.name}」创建自由会话 · 该 agent 不在「${projectContext.projectName}」项目里`,
    );
  }
  store.clear();
  dispatchFreeChat(item, ctx);
}

export const newProjectChatSource: CommandSource<NewProjectChatItem> = {
  id: "new-project-chat",
  heading: "项目新对话",
  modes: ["command"],
  activeFor: (ctx) => isProjectsRoute(ctx.pathname),
  useItems,
  getScore,
  renderItem,
  onSelect,
};
