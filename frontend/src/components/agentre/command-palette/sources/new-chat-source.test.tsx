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
  newChatSource,
  type NewChatItem,
} from "./new-chat-source";
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

function mkItem(over: Partial<NewChatItem> = {}): NewChatItem {
  const agent = over.agent ?? mkAgent({ id: 7, name: "X" });
  return {
    key: `new-chat-agent-${agent.id}`,
    agentId: agent.id,
    agent,
    isMember: true,
    ...over,
  };
}

function mkCtx(pathname = "/chat") {
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

describe("flattenAgents (NewChatSource · 自由会话)", () => {
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
    const items = flattenAgents(agents);
    expect(items.map((i) => i.agent.id)).toEqual([1]);
  });

  it("pinned agents come before non-pinned; otherwise input order is preserved", () => {
    const agents: ChatAgentItem[] = [
      mkAgent({ id: 1, name: "A", pinned: false }),
      mkAgent({ id: 2, name: "B", pinned: true }),
      mkAgent({ id: 3, name: "C", pinned: false }),
      mkAgent({ id: 4, name: "D", pinned: true }),
    ];
    const items = flattenAgents(agents);
    expect(items.map((i) => i.agent.id)).toEqual([2, 4, 1, 3]);
  });

  it("item key is stable and namespaced", () => {
    const items = flattenAgents([mkAgent({ id: 42 })]);
    expect(items[0]?.key).toBe("new-chat-agent-42");
  });

  it("all items marked isMember=true (无项目分组概念)", () => {
    const items = flattenAgents([mkAgent({ id: 1 }), mkAgent({ id: 2 })]);
    expect(items.every((i) => i.isMember)).toBe(true);
  });

  it("lastAgentId 命中时冒泡到组首，破坏 pinned 优先", () => {
    const agents: ChatAgentItem[] = [
      mkAgent({ id: 1, name: "A", pinned: true }),
      mkAgent({ id: 2, name: "B", pinned: false }),
      mkAgent({ id: 3, name: "C", pinned: false }),
    ];
    const items = flattenAgents(agents, 3);
    expect(items.map((i) => i.agent.id)).toEqual([3, 1, 2]);
  });

  it("lastAgentId 不在 chattable 列表里时（被删 / 不存在）退化为默认排序", () => {
    const agents: ChatAgentItem[] = [
      mkAgent({ id: 1, name: "A", pinned: false }),
      mkAgent({ id: 2, name: "B", pinned: true }),
    ];
    const items = flattenAgents(agents, 999);
    expect(items.map((i) => i.agent.id)).toEqual([2, 1]);
  });

  it("lastAgentId === null（参数缺省）保持历史排序", () => {
    const agents: ChatAgentItem[] = [
      mkAgent({ id: 1, name: "A", pinned: false }),
      mkAgent({ id: 2, name: "B", pinned: true }),
    ];
    const items = flattenAgents(agents);
    expect(items.map((i) => i.agent.id)).toEqual([2, 1]);
  });
});

describe("newChatSource — metadata", () => {
  it("declares modes=['command']", () => {
    expect(newChatSource.modes).toEqual(["command"]);
  });

  it("activeFor returns true for non-/projects routes", () => {
    expect(newChatSource.activeFor?.({ pathname: "/chat" })).toBe(true);
    expect(newChatSource.activeFor?.({ pathname: "/" })).toBe(true);
    expect(newChatSource.activeFor?.({ pathname: "/issues" })).toBe(true);
  });

  it("activeFor returns false for /projects routes (互斥于 newProjectChatSource)", () => {
    expect(newChatSource.activeFor?.({ pathname: "/projects" })).toBe(false);
    expect(newChatSource.activeFor?.({ pathname: "/projects/42" })).toBe(false);
    expect(newChatSource.activeFor?.({ pathname: "/projects/42/foo" })).toBe(
      false,
    );
  });

  it("getScore matches the full action title (delegates to scoreItem)", () => {
    const item = mkItem({ agent: mkAgent({ id: 7, name: "CEO 助手" }) });
    expect(newChatSource.getScore("CEO", item)).toBeGreaterThan(0);
    expect(newChatSource.getScore("New chat", item)).toBeGreaterThan(0);
    expect(newChatSource.getScore("xyz-nope", item)).toBe(0);
    expect(newChatSource.getScore("", item)).toBe(1);
  });
});

describe("newChatSource — search behavior (BDD: full-action-title match, case-insensitive)", () => {
  // VSCode-style：每行整体是一个动作 "New chat with <agent.name>"，
  // 搜索按完整标题做模糊匹配。
  const ceoItem = () => mkItem({ agent: mkAgent({ id: 1, name: "CEO 助手" }) });
  const engItem = () => mkItem({ agent: mkAgent({ id: 2, name: "工程师" }) });
  const designerItem = () =>
    mkItem({ agent: mkAgent({ id: 3, name: "Designer" }) });

  it("empty payload → all items pass (list everything in command mode)", () => {
    expect(newChatSource.getScore("", ceoItem())).toBeGreaterThan(0);
    expect(newChatSource.getScore("", engItem())).toBeGreaterThan(0);
  });

  it("query is the command-name prefix 'new chat' → all chattable items match", () => {
    expect(newChatSource.getScore("new chat", ceoItem())).toBeGreaterThan(0);
    expect(newChatSource.getScore("new chat", engItem())).toBeGreaterThan(0);
    expect(newChatSource.getScore("new chat", designerItem())).toBeGreaterThan(
      0,
    );
  });

  it("case-insensitive: 'NEW CHAT' / 'NeW cHaT' / 'new chat' all match", () => {
    expect(newChatSource.getScore("NEW CHAT", ceoItem())).toBeGreaterThan(0);
    expect(newChatSource.getScore("NeW cHaT", ceoItem())).toBeGreaterThan(0);
    expect(newChatSource.getScore("new chat", ceoItem())).toBeGreaterThan(0);
  });

  it("command-name + agent narrows to one: 'new chat ce' only matches CEO", () => {
    expect(newChatSource.getScore("new chat ce", ceoItem())).toBeGreaterThan(0);
    expect(newChatSource.getScore("new chat ce", engItem())).toBe(0);
  });

  it("agent-name only (English): 'CEO' / 'ceo' matches CEO 助手 via substring", () => {
    expect(newChatSource.getScore("CEO", ceoItem())).toBeGreaterThan(0);
    expect(newChatSource.getScore("ceo", ceoItem())).toBeGreaterThan(0);
    expect(newChatSource.getScore("CEO", engItem())).toBe(0);
  });

  it("agent-name only (Chinese): '工程师' matches via substring in title", () => {
    expect(newChatSource.getScore("工程师", engItem())).toBeGreaterThan(0);
    expect(newChatSource.getScore("工程师", ceoItem())).toBe(0);
  });

  it("totally unrelated query returns 0", () => {
    expect(newChatSource.getScore("zzz-no-such-agent", ceoItem())).toBe(0);
  });

  // "new project chat" 这种项目命令名片段不应在 newChatSource 命中
  it("does NOT match 'new project chat' query (that's the other source's command name)", () => {
    expect(newChatSource.getScore("new project chat", ceoItem())).toBe(0);
  });
});

describe("newChatSource.renderItem — shows 'New chat with <name>' label", () => {
  it("renders the action prefix so users see what command they're invoking", () => {
    const item = mkItem({ agent: mkAgent({ id: 1, name: "CEO 助手" }) });
    const { container } = render(
      <>{newChatSource.renderItem(item, { active: false })}</>,
    );
    expect(container.textContent).toContain("New chat with");
    expect(container.textContent).toContain("CEO 助手");
    // 不应包含 "New project chat with" —— 那是 newProjectChatSource 的命令名
    expect(container.textContent).not.toContain("New project chat with");
  });
});

describe("newChatSource.onSelect — 永远走 /chat 自由会话，忽略 store", () => {
  beforeEach(() => {
    useNewChatContextStore.getState().clear();
    clearLastAgentId();
  });
  afterEach(() => {
    clearLastAgentId();
  });

  it("写入 lastAgentId，供下次面板打开时置顶", () => {
    const item = mkItem({ agent: mkAgent({ id: 42, name: "工程师" }) });
    const ctx = mkCtx("/chat");
    newChatSource.onSelect(item, ctx);
    expect(readLastAgentId()).toBe(42);
  });

  it("dispatches free chat regardless of any project context in store", () => {
    // 模拟：store 里有项目页残留的 context + handler（不该影响自由会话）
    const handler = vi.fn();
    useNewChatContextStore.getState().setContext({
      projectID: 42,
      projectName: "后端重构",
    });
    useNewChatContextStore.getState().setNewSelectionHandler(handler);

    const item = mkItem({ agent: mkAgent({ id: 7, name: "CEO" }) });
    const ctx = mkCtx("/chat");
    newChatSource.onSelect(item, ctx);

    expect(handler).not.toHaveBeenCalled();
    expect(ctx.openNewSession).toHaveBeenCalledWith(7);
    expect(ctx.openSession).not.toHaveBeenCalled();
    expect(ctx.navigate).toHaveBeenCalledWith("/chat");
  });

  it("from any non-/projects route also goes free", () => {
    const item = mkItem({ agent: mkAgent({ id: 7 }) });
    const ctx = mkCtx("/issues");
    newChatSource.onSelect(item, ctx);

    expect(ctx.openNewSession).toHaveBeenCalledWith(7);
    expect(ctx.navigate).toHaveBeenCalledWith("/chat");
  });
});
