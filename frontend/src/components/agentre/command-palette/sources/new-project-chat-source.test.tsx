import { render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { ChatAgentItem } from "@/hooks/use-chat-agents";
import {
  clearLastAgentId,
  readLastAgentId,
} from "@/stores/last-agent-persistence";
import { useNewChatContextStore } from "@/stores/new-chat-context-store";

import {
  flattenAgents,
  newProjectChatSource,
  type NewProjectChatItem,
} from "./new-project-chat-source";
import type { OnSelectCtx } from "../types";

function mkAgent(over: Partial<ChatAgentItem> = {}): ChatAgentItem {
  return {
    id: 1,
    name: "Agent",
    avatarColor: "agent-1",
    avatarIcon: "",
    avatarDataUrl: "",
    backendType: "claudecode",
    chattable: true,
    pinned: false,
    chattableHint: "",
    activeCount: 0,
    recentCount: 0,
    totalSessions: 0,
    sessions: [],
    attentionSessions: [],
    ...over,
  } as ChatAgentItem;
}

function mkItem(over: Partial<NewProjectChatItem> = {}): NewProjectChatItem {
  const agent = over.agent ?? mkAgent({ id: 7, name: "X" });
  return {
    key: `new-project-chat-agent-${agent.id}`,
    agentId: agent.id,
    agent,
    isMember: true,
    ...over,
  };
}

function mkCtx(pathname = "/projects") {
  return {
    navigate: vi.fn(),
    close: vi.fn(),
    openSession: vi.fn(),
    openNewSession: vi.fn(),
    pathname,
  } as unknown as OnSelectCtx & {
    navigate: ReturnType<typeof vi.fn>;
    close: ReturnType<typeof vi.fn>;
    openSession: ReturnType<typeof vi.fn>;
    openNewSession: ReturnType<typeof vi.fn>;
  };
}

describe("flattenAgents (NewProjectChatSource · 项目分组)", () => {
  it("only chattable agents are kept", () => {
    const agents: ChatAgentItem[] = [
      mkAgent({ id: 1, name: "Yes", chattable: true }),
      mkAgent({
        id: 2,
        name: "No",
        chattable: false,
        chattableHint: "未绑后端",
      }),
    ];
    const items = flattenAgents(agents, null);
    expect(items.map((i) => i.agent.id)).toEqual([1]);
  });

  it("with no members set: all marked isMember=true (项目页未选项目时的退化形态)", () => {
    const agents: ChatAgentItem[] = [
      mkAgent({ id: 1, name: "A" }),
      mkAgent({ id: 2, name: "B" }),
    ];
    const items = flattenAgents(agents, null);
    expect(items.every((i) => i.isMember)).toBe(true);
  });

  it("pinned-first holds within the 'no members' branch", () => {
    const agents: ChatAgentItem[] = [
      mkAgent({ id: 1, name: "A", pinned: false }),
      mkAgent({ id: 2, name: "B", pinned: true }),
    ];
    const items = flattenAgents(agents, null);
    expect(items.map((i) => i.agent.id)).toEqual([2, 1]);
  });

  it("with members set: groups members first, non-members after; non-members marked isMember=false", () => {
    const agents: ChatAgentItem[] = [
      mkAgent({ id: 1, name: "A" }),
      mkAgent({ id: 2, name: "B" }),
      mkAgent({ id: 3, name: "C" }),
    ];
    const members = new Set([2, 3]);
    const items = flattenAgents(agents, members);
    expect(items.map((i) => ({ id: i.agent.id, m: i.isMember }))).toEqual([
      { id: 2, m: true },
      { id: 3, m: true },
      { id: 1, m: false },
    ]);
  });

  it("with members set: pinned-non-member still goes to non-member group", () => {
    const agents: ChatAgentItem[] = [
      mkAgent({ id: 1, name: "A", pinned: true }),
      mkAgent({ id: 2, name: "B", pinned: false }),
    ];
    const members = new Set([2]);
    const items = flattenAgents(agents, members);
    expect(items.map((i) => ({ id: i.agent.id, m: i.isMember }))).toEqual([
      { id: 2, m: true },
      { id: 1, m: false },
    ]);
  });

  it("item key is stable and namespaced (new-project-chat-agent-N)", () => {
    const items = flattenAgents([mkAgent({ id: 42 })], null);
    expect(items[0]?.key).toBe("new-project-chat-agent-42");
  });

  it("无 members（退化模式）：lastAgentId 命中冒泡到组首", () => {
    const agents: ChatAgentItem[] = [
      mkAgent({ id: 1, name: "A", pinned: true }),
      mkAgent({ id: 2, name: "B", pinned: false }),
      mkAgent({ id: 3, name: "C", pinned: false }),
    ];
    const items = flattenAgents(agents, null, 3);
    expect(items.map((i) => i.agent.id)).toEqual([3, 1, 2]);
  });

  it("有 members：lastAgentId 是 member 时冒泡到 member 组首", () => {
    const agents: ChatAgentItem[] = [
      mkAgent({ id: 1, name: "A", pinned: true }),
      mkAgent({ id: 2, name: "B" }),
      mkAgent({ id: 3, name: "C" }),
    ];
    const members = new Set([1, 2]);
    const items = flattenAgents(agents, members, 2);
    expect(items.map((i) => ({ id: i.agent.id, m: i.isMember }))).toEqual([
      { id: 2, m: true },
      { id: 1, m: true },
      { id: 3, m: false },
    ]);
  });

  it("有 members：lastAgentId 不是 member 时不冒泡（members-first 是更强语义）", () => {
    const agents: ChatAgentItem[] = [
      mkAgent({ id: 1, name: "A" }),
      mkAgent({ id: 2, name: "B" }),
      mkAgent({ id: 3, name: "C" }),
    ];
    const members = new Set([1, 2]);
    const items = flattenAgents(agents, members, 3);
    expect(items.map((i) => ({ id: i.agent.id, m: i.isMember }))).toEqual([
      { id: 1, m: true },
      { id: 2, m: true },
      { id: 3, m: false },
    ]);
  });

  describe("paths arg (device-aware path preview)", () => {
    it("local agent · resolves to paths.localPath", () => {
      const agents: ChatAgentItem[] = [
        mkAgent({ id: 1, name: "L", deviceID: "" }),
      ];
      const items = flattenAgents(agents, null, null, {
        localPath: "/Code/foo",
      });
      expect(items[0]?.locationPath).toBe("/Code/foo");
    });

    it("remote agent · resolves to paths.byDeviceID[deviceID]", () => {
      const agents: ChatAgentItem[] = [
        mkAgent({ id: 1, name: "R", deviceID: "7", deviceName: "linux-srv" }),
      ];
      const items = flattenAgents(agents, null, null, {
        byDeviceID: { "7": "/home/me/foo" },
      });
      expect(items[0]?.locationPath).toBe("/home/me/foo");
    });

    it("remote agent · missing location maps to undefined (UI 渲染时跳过 cwd 预览)", () => {
      const agents: ChatAgentItem[] = [
        mkAgent({ id: 1, name: "R", deviceID: "7" }),
      ];
      const items = flattenAgents(agents, null, null, { byDeviceID: {} });
      expect(items[0]?.locationPath).toBeUndefined();
    });

    it("paths=undefined · 全部不带 locationPath(退化/无 project context)", () => {
      const agents: ChatAgentItem[] = [
        mkAgent({ id: 1, name: "L", deviceID: "" }),
        mkAgent({ id: 2, name: "R", deviceID: "7" }),
      ];
      const items = flattenAgents(agents, null);
      expect(items.every((i) => i.locationPath === undefined)).toBe(true);
    });
  });

  describe("subHeading (两段分组)", () => {
    it("projectName 提供时:成员的 subHeading 是 '在 {N} 中新建 chat',非成员是 '其它 Agent'", () => {
      const agents: ChatAgentItem[] = [
        mkAgent({ id: 1, name: "A" }),
        mkAgent({ id: 2, name: "B" }),
      ];
      const members = new Set([1]);
      const items = flattenAgents(agents, members, null, undefined, "foo");
      expect(items.map((i) => i.subHeading)).toEqual([
        "New chat in foo",
        "Other Agents",
      ]);
    });

    it("projectName=undefined 时所有 item 不带 subHeading(单组)", () => {
      const agents: ChatAgentItem[] = [
        mkAgent({ id: 1, name: "A" }),
        mkAgent({ id: 2, name: "B" }),
      ];
      const members = new Set([1]);
      const items = flattenAgents(agents, members);
      expect(items.every((i) => i.subHeading === undefined)).toBe(true);
    });
  });
});

describe("newProjectChatSource — metadata", () => {
  it("declares modes=['command']", () => {
    expect(newProjectChatSource.modes).toEqual(["command"]);
  });

  it("activeFor returns true for /projects routes only", () => {
    expect(newProjectChatSource.activeFor?.({ pathname: "/projects" })).toBe(
      true,
    );
    expect(newProjectChatSource.activeFor?.({ pathname: "/projects/42" })).toBe(
      true,
    );
    expect(
      newProjectChatSource.activeFor?.({ pathname: "/projects/42/foo" }),
    ).toBe(true);
  });

  it("activeFor returns false for non-/projects routes (互斥于 newChatSource)", () => {
    expect(newProjectChatSource.activeFor?.({ pathname: "/chat" })).toBe(false);
    expect(newProjectChatSource.activeFor?.({ pathname: "/" })).toBe(false);
    expect(newProjectChatSource.activeFor?.({ pathname: "/issues" })).toBe(
      false,
    );
  });

  it("getScore matches 'New project chat with X' full title", () => {
    const item = mkItem({ agent: mkAgent({ id: 7, name: "CEO 助手" }) });
    expect(newProjectChatSource.getScore("CEO", item)).toBeGreaterThan(0);
    expect(
      newProjectChatSource.getScore("New project chat", item),
    ).toBeGreaterThan(0);
    expect(newProjectChatSource.getScore("xyz-nope", item)).toBe(0);
    expect(newProjectChatSource.getScore("", item)).toBe(1);
  });

  it("'new chat' also matches because 'new chat' is substring of title 'New project chat with X'", () => {
    const item = mkItem({ agent: mkAgent({ id: 7, name: "CEO" }) });
    expect(newProjectChatSource.getScore("new chat", item)).toBeGreaterThan(0);
  });
});

describe("newProjectChatSource.renderItem — shows 'New project chat with <name>'", () => {
  it("renders the project-scoped command name", () => {
    const item = mkItem({ agent: mkAgent({ id: 1, name: "CEO 助手" }) });
    const { container } = render(
      <>{newProjectChatSource.renderItem(item, { active: false })}</>,
    );
    expect(container.textContent).toContain("New project chat with");
    expect(container.textContent).toContain("CEO 助手");
  });

  it("non-member rows show 'Not in this project' badge", () => {
    const item = mkItem({
      agent: mkAgent({ id: 5, name: "Designer" }),
      isMember: false,
    });
    const { container } = render(
      <>{newProjectChatSource.renderItem(item, { active: false })}</>,
    );
    expect(container.textContent).toContain("Not in this project");
  });
});

describe("newProjectChatSource.onSelect — 项目作用域分发", () => {
  beforeEach(() => {
    useNewChatContextStore.getState().clear();
    clearLastAgentId();
    vi.spyOn(console, "info").mockImplementation(() => {});
  });
  afterEach(() => {
    clearLastAgentId();
  });

  it("写入 lastAgentId（不论走项目路径还是兜底自由会话）", () => {
    // 项目路径：handler 注册 + member
    const handler = vi.fn();
    useNewChatContextStore
      .getState()
      .setContext({ projectID: 42, projectName: "X" });
    useNewChatContextStore.getState().setNewSelectionHandler(handler);
    const agent = mkAgent({ id: 77 });
    newProjectChatSource.onSelect(
      mkItem({ agent, isMember: true }),
      mkCtx("/projects"),
    );
    expect(readLastAgentId()).toBe(77);

    // 兜底自由会话
    useNewChatContextStore.getState().clear();
    clearLastAgentId();
    newProjectChatSource.onSelect(
      mkItem({ agent: mkAgent({ id: 88 }), isMember: false }),
      mkCtx("/projects"),
    );
    expect(readLastAgentId()).toBe(88);
  });

  it("defense: if pathname is somehow not /projects → free chat fallback (activeFor 已经过滤了，这里只是兜底)", () => {
    const item = mkItem({ agent: mkAgent({ id: 7 }) });
    const ctx = mkCtx("/chat");
    newProjectChatSource.onSelect(item, ctx);

    expect(ctx.openNewSession).toHaveBeenCalledWith(7);
    expect(ctx.navigate).toHaveBeenCalledWith("/chat");
  });

  it("with no project context (tree not yet selected) → free chat fallback", () => {
    const item = mkItem({ agent: mkAgent({ id: 7 }) });
    const ctx = mkCtx("/projects");
    newProjectChatSource.onSelect(item, ctx);

    expect(ctx.openNewSession).toHaveBeenCalledWith(7);
    expect(ctx.navigate).toHaveBeenCalledWith("/chat");
  });

  it("with projectContext + isMember=true → handler called, no navigate", () => {
    const handler = vi.fn();
    useNewChatContextStore.getState().setContext({
      projectID: 42,
      projectName: "后端重构",
    });
    useNewChatContextStore.getState().setNewSelectionHandler(handler);

    const agent = mkAgent({ id: 7, name: "CEO" });
    const item = mkItem({ agent, isMember: true });
    const ctx = mkCtx("/projects");
    newProjectChatSource.onSelect(item, ctx);

    expect(ctx.close).toHaveBeenCalledTimes(1);
    expect(handler).toHaveBeenCalledWith(42, agent);
    expect(ctx.navigate).not.toHaveBeenCalled();
    expect(ctx.openNewSession).not.toHaveBeenCalled();
    expect(ctx.openSession).not.toHaveBeenCalled();
  });

  it("non-member silent fallback → /chat + console.info + clears store", () => {
    useNewChatContextStore.getState().setContext({
      projectID: 42,
      projectName: "后端重构",
    });
    useNewChatContextStore.getState().setNewSelectionHandler(vi.fn());

    const agent = mkAgent({ id: 9, name: "设计师" });
    const item = mkItem({ agent, isMember: false });
    const ctx = mkCtx("/projects");
    newProjectChatSource.onSelect(item, ctx);

    expect(ctx.openNewSession).toHaveBeenCalledWith(9);
    expect(ctx.navigate).toHaveBeenCalledWith("/chat");
    expect(console.info).toHaveBeenCalled();
    expect(useNewChatContextStore.getState().projectContext).toBeNull();
  });

  it("isMember=true but handler not registered (init race) → falls back to free chat", () => {
    useNewChatContextStore.getState().setContext({
      projectID: 42,
      projectName: "X",
    });
    // handler intentionally not set

    const item = mkItem({ agent: mkAgent({ id: 7 }), isMember: true });
    const ctx = mkCtx("/projects");
    newProjectChatSource.onSelect(item, ctx);

    expect(ctx.openNewSession).toHaveBeenCalledWith(7);
    expect(ctx.navigate).toHaveBeenCalledWith("/chat");
  });

  it("nested project route /projects/42/foo also dispatches as projects", () => {
    const handler = vi.fn();
    useNewChatContextStore.getState().setContext({
      projectID: 42,
      projectName: "X",
    });
    useNewChatContextStore.getState().setNewSelectionHandler(handler);

    const agent = mkAgent({ id: 7 });
    newProjectChatSource.onSelect(
      mkItem({ agent, isMember: true }),
      mkCtx("/projects/42"),
    );
    expect(handler).toHaveBeenCalledWith(42, agent);
  });
});
