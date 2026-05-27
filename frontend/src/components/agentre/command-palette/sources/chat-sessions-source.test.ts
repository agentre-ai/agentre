import { describe, expect, it, vi } from "vitest";

import type { ChatAgentItem } from "@/hooks/use-chat-agents";

import { chatSessionsSource, flattenSessions } from "./chat-sessions-source";

type SessionLite = ChatAgentItem["sessions"][number];

function mkSession(over: Partial<SessionLite> = {}): SessionLite {
  return {
    id: 1,
    title: "session-1",
    status: "idle",
    needsAttention: false,
    lastMessageAt: 1700000000000,
    ...over,
  } as SessionLite;
}

function mkAgent(over: Partial<ChatAgentItem> = {}): ChatAgentItem {
  return {
    id: 1,
    name: "Agent",
    avatarColor: "agent-1",
    avatarIcon: "",
    avatarDataUrl: "",
    backendType: "",
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

describe("flattenSessions", () => {
  it("flattens agents.sessions into base items with active=false (attention computed separately in useItems)", () => {
    const agents: ChatAgentItem[] = [
      mkAgent({
        id: 1,
        name: "CEO 助手",
        sessions: [
          mkSession({ id: 11, title: "闲", status: "idle", lastMessageAt: 5 }),
          mkSession({
            id: 12,
            title: "跑",
            status: "running",
            lastMessageAt: 1,
          }),
        ],
      }),
      mkAgent({
        id: 2,
        name: "Other",
        sessions: [
          mkSession({
            id: 21,
            title: "等",
            status: "waiting",
            lastMessageAt: 2,
          }),
          mkSession({
            id: 22,
            title: "旧",
            status: "idle",
            lastMessageAt: 10,
          }),
        ],
      }),
    ];
    const items = flattenSessions(agents);
    // flattenSessions 不再做 attention 排序；只做展开 + 字段映射
    expect(items.map((i) => i.sessionId)).toEqual([11, 12, 21, 22]);
    // active=false for all (attention computed in useItems via useSessionAttentionList)
    expect(items.every((i) => i.active === false)).toBe(true);
    expect(items.every((i) => i.attentionReason === null)).toBe(true);
  });

  it("fallback title for empty string", () => {
    const items = flattenSessions([
      mkAgent({ sessions: [mkSession({ id: 7, title: "" })] }),
    ]);
    expect(items[0].title).toBe("(未命名会话)");
  });
});

describe("chatSessionsSource.getScore", () => {
  it("boosts active sessions by +5", () => {
    const idleItem = {
      key: "k1",
      sessionId: 1,
      agentId: 1,
      title: "年度报告",
      agentName: "CEO",
      agentColor: "agent-1" as const,
      status: "idle" as const,
      lastMessageAt: 0,
      active: false,
      attentionReason: null as null,
    };
    const activeItem = {
      ...idleItem,
      active: true,
      attentionReason: "running" as const,
    };
    expect(chatSessionsSource.getScore("年度", idleItem)).toBe(80);
    expect(chatSessionsSource.getScore("年度", activeItem)).toBe(85);
  });

  it("zero score never gets boosted (still 0)", () => {
    const item = {
      key: "k",
      sessionId: 1,
      agentId: 1,
      title: "年度报告",
      agentName: "CEO",
      agentColor: "agent-1" as const,
      status: "running" as const,
      lastMessageAt: 0,
      active: true,
      attentionReason: "running" as const,
    };
    expect(chatSessionsSource.getScore("zzzz", item)).toBe(0);
  });
});

describe("chatSessionsSource.onSelect", () => {
  it("opens the session tab, closes palette, then navigates to /chat", () => {
    const openSession = vi.fn();
    const openNewSession = vi.fn();
    const close = vi.fn();
    const navigate = vi.fn();
    const item = {
      key: "k",
      sessionId: 99,
      agentId: 7,
      title: "x",
      agentName: "a",
      agentColor: "agent-1" as const,
      status: "idle" as const,
      lastMessageAt: 0,
      active: false,
      attentionReason: null as null,
    };
    chatSessionsSource.onSelect(item, {
      openSession,
      openNewSession,
      close,
      navigate: navigate as never,
      pathname: "/chat",
    });
    expect(openSession).toHaveBeenCalledWith(99);
    expect(openNewSession).not.toHaveBeenCalled();
    expect(close).toHaveBeenCalled();
    expect(navigate).toHaveBeenCalledWith("/chat");
  });

  it("swallows navigate errors without throwing", () => {
    const item = {
      key: "k",
      sessionId: 1,
      agentId: 1,
      title: "x",
      agentName: "a",
      agentColor: "agent-1" as const,
      status: "idle" as const,
      lastMessageAt: 0,
      active: false,
      attentionReason: null as null,
    };
    const navigate = vi.fn(() => {
      throw new Error("boom");
    });
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    expect(() =>
      chatSessionsSource.onSelect(item, {
        openSession: vi.fn(),
        openNewSession: vi.fn(),
        close: vi.fn(),
        navigate: navigate as never,
        pathname: "/chat",
      }),
    ).not.toThrow();
    expect(warn).toHaveBeenCalled();
    warn.mockRestore();
  });
});
