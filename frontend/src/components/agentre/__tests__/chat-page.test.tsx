import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useCommandPaletteStore } from "@/stores/command-palette-store";
import { useChatTabsStore } from "@/stores/chat-tabs-store";
import { useGroupListStore } from "@/stores/group-list-store";

function resetTabsStore() {
  useChatTabsStore.setState({ tabs: [], activeTabId: null });
}

// selectSession: 在 chat-tabs-store 里塞一个 active "session" tab。
// chat-page 的 selectedSessionId/selectedAgentId 完全派生自 active tab,
// 所以 sidebar 的「打开了哪条会话」高亮就靠这个 helper 注入。
function selectSession(sessionId: number) {
  const tab = {
    id: `seed-${sessionId}`,
    meta: { kind: "session" as const, sessionId },
    isPreview: false,
    isPinned: false,
    pinAt: 0,
    openedAt: 0,
  };
  useChatTabsStore.setState({ tabs: [tab], activeTabId: tab.id });
}

const appMocks = vi.hoisted(() => ({
  CancelQueuedChatMessage: vi.fn(),
  DeleteChatSession: vi.fn(),
  EditChatMessage: vi.fn(),
  EnqueueChatMessage: vi.fn(),
  ListChatAgents: vi.fn(),
  LoadChatSession: vi.fn(),
  MarkChatSessionRead: vi.fn().mockResolvedValue({}),
  RegenerateChatMessage: vi.fn(),
  RenameChatSession: vi.fn(),
  SendChatMessage: vi.fn(),
  ListChatAgentSessions: vi
    .fn()
    .mockResolvedValue({ sessions: [], total: 0, hasMore: false }),
  GroupList: vi.fn().mockResolvedValue([]),
}));

const runtimeMocks = vi.hoisted(() => {
  const handlers = new Map<string, (payload: unknown) => void>();
  return {
    handlers,
    EventsOff: vi.fn((eventName: string) => {
      handlers.delete(eventName);
    }),
    EventsOn: vi.fn(
      (eventName: string, callback: (payload: unknown) => void) => {
        handlers.set(eventName, callback);
      },
    ),
  };
});

vi.mock("../../../../wailsjs/go/app/App", () => appMocks);

vi.mock("../../../../wailsjs/runtime/runtime", () => ({
  EventsOff: runtimeMocks.EventsOff,
  EventsOn: runtimeMocks.EventsOn,
}));

import { ChatPage } from "../chat-page";

// renderChatPage: ChatPage は sidebar のみをレンダリングするので、
// ChatStreamsHost は不要になった。
function renderChatPage() {
  return render(<ChatPage />);
}

describe("ChatPage sidebar — attention bubble", () => {
  beforeEach(() => {
    resetTabsStore();
    selectSession(1);
    runtimeMocks.handlers.clear();
    vi.clearAllMocks();

    appMocks.ListChatAgents.mockResolvedValue({
      agents: [
        {
          activeCount: 0,
          avatarColor: "agent-1",
          backendType: "builtin",
          chattable: true,
          id: 7,
          name: "Eng",
          pinned: false,
          recentCount: 1,
          sessions: [
            { id: 1, lastMessageAt: 0, status: "idle", title: "Debug session" },
          ],
        },
      ],
    });
  });

  afterEach(() => {
    localStorage.clear();
  });

  it("keeps the selected session pinned in the collapsed attention bubble", async () => {
    // 单 agent + 单个 idle 会话。idle 不在 needs_attention/running/error 范畴；
    // 没有 active tab 时折叠态 bubble 应为空；
    // 点击 header 打开它后，bubble 应固定显示这条会话。
    resetTabsStore();
    // 默认 ListChatAgents mock 里 session 1 的 lastMessageAt=0；lastReadAt 默认 0 →
    // rankAttention 判 0>0=false，本身就不会归到 'unread'，无需额外设置已读状态。

    renderChatPage();

    const header = await screen.findByRole("button", {
      name: "Open Eng recent session",
    });

    // 初始：bubble 隐藏（idle + 已读 + 未选中）。
    expect(
      document.querySelector('[data-slot="agent-attention-bubble"]'),
    ).toBeNull();

    fireEvent.click(header);

    // 选中后：bubble 出现，里面有 "Debug session"。
    const bubble = await waitFor(() => {
      const node = document.querySelector(
        '[data-slot="agent-attention-bubble"]',
      );
      expect(node).not.toBeNull();
      return node!;
    });
    expect(bubble).toHaveTextContent("Debug session");
  });
});

describe("ChatPage attention bubble expanded-state filtering", () => {
  // 单独测「selected rank 在 expanded 时被过滤」—— 上一组测试 cover 了 unread，
  // 这里 cover 同样走 visibleAttention 过滤的 selected 分支。
  beforeEach(() => {
    resetTabsStore();
    selectSession(9);
    localStorage.setItem("agentre.agentExpanded.agent:7", "1");
    // 把 selected 那条会话标记成已读（通过 mock 数据上的 lastReadAt），避免被误判成 unread。
    runtimeMocks.handlers.clear();
    vi.clearAllMocks();
    appMocks.LoadChatSession?.mockReset?.();
    appMocks.ListChatAgents.mockReset();
  });

  afterEach(() => {
    localStorage.clear();
  });

  it("drops a 'selected' idle entry from the bubble when the group is expanded", async () => {
    appMocks.ListChatAgents.mockResolvedValue({
      agents: [
        {
          activeCount: 0,
          // 后端 attention 池里没东西（idle 不进后端 list）
          attentionSessions: [],
          avatarColor: "agent-1",
          backendType: "builtin",
          chattable: true,
          id: 7,
          name: "Eng",
          pinned: false,
          recentCount: 1,
          // 选中的 idle 会话；buildAttentionSessions 会给它打上 rank="selected"。
          // 展开态下应该被 visibleAttention 滤掉，bubble 整个不渲染。
          sessions: [
            {
              id: 9,
              lastMessageAt: 9999,
              lastReadAt: 9999,
              status: "idle",
              title: "Selected idle",
            },
          ],
        },
      ],
    });

    renderChatPage();

    // 等 sidebar 把会话行渲染出来
    await screen.findByRole("button", { name: /Selected idle/ });

    // bubble 不该出现（唯一候选是 selected，被过滤后 visibleAttention 为空）。
    expect(
      document.querySelector('[data-slot="agent-attention-bubble"]'),
    ).toBeNull();

    // 但该会话仍然在下方常规列表里能看到（dedupe 没把它误删）。
    expect(
      screen.getByRole("button", { name: /Selected idle/ }),
    ).toBeInTheDocument();
  });
});

describe("ChatPage attention bubble always visible + dedupe", () => {
  // 用户诉求：running / 未读 会话要在 sidebar 一直可见，无论分组是否展开；
  // bubble 和下方常规 5 行列表对同一 sessionId 不能同时各渲染一份。
  beforeEach(() => {
    resetTabsStore();
    selectSession(1);
    // 预展开 agent:7，进入 expanded 分支验证 bubble 仍在
    localStorage.setItem("agentre.agentExpanded.agent:7", "1");
    runtimeMocks.handlers.clear();
    vi.clearAllMocks();
    appMocks.LoadChatSession?.mockReset?.();
    appMocks.ListChatAgents.mockReset();
  });

  afterEach(() => {
    localStorage.clear();
  });

  it("renders the attention bubble when the group is expanded", async () => {
    appMocks.ListChatAgents.mockResolvedValue({
      agents: [
        {
          activeCount: 1,
          attentionSessions: [
            {
              id: 1,
              lastMessageAt: 2000,
              needsAttention: false,
              status: "running",
              title: "Debug session",
            },
          ],
          avatarColor: "agent-1",
          backendType: "builtin",
          chattable: true,
          id: 7,
          name: "Eng",
          pinned: false,
          recentCount: 2,
          sessions: [
            {
              id: 1,
              lastMessageAt: 2000,
              status: "running",
              title: "Debug session",
            },
            {
              id: 2,
              lastMessageAt: 1000,
              status: "idle",
              title: "Other session",
            },
          ],
        },
      ],
    });

    renderChatPage();

    // bubble 必须在展开态也渲染（之前会被 !expanded 守卫掉）
    const bubble = await waitFor(() => {
      const node = document.querySelector(
        '[data-slot="agent-attention-bubble"]',
      );
      expect(node).not.toBeNull();
      return node!;
    });
    expect(bubble).toHaveTextContent("Debug session");
  });

  it("collapsed: shows an idle unread session in the bubble with orange (waiting) status", async () => {
    // 折叠（不预设 agentExpanded localStorage）—— 把 idle 但未读的会话放到 bubble，
    // 用橙色（statusConfig.waiting）标记，trailingLabel="未读"。
    localStorage.removeItem("agentre.agentExpanded.agent:7");
    // session.lastMessageAt=2000 > session.lastReadAt=0(默认) → 触发 unread 判定。

    appMocks.ListChatAgents.mockResolvedValue({
      agents: [
        {
          activeCount: 0,
          // 后端的 attentionSessions 不含 idle —— 前端要自己从 a.sessions 里补
          attentionSessions: [],
          avatarColor: "agent-1",
          backendType: "builtin",
          chattable: true,
          id: 7,
          name: "Eng",
          pinned: false,
          recentCount: 1,
          sessions: [
            {
              id: 5,
              lastMessageAt: 2000,
              status: "idle",
              title: "Background done",
            },
          ],
        },
      ],
    });

    renderChatPage();

    const bubble = await waitFor(() => {
      const node = document.querySelector(
        '[data-slot="agent-attention-bubble"]',
      );
      expect(node).not.toBeNull();
      return node!;
    });
    expect(bubble).toHaveTextContent("Background done");
    expect(bubble).toHaveTextContent("Unread");
    // bubble 里的 SessionRow 内 StatusDot 用 bg-status-waiting（橙）
    const dot = bubble.querySelector('[aria-label="waiting status"]');
    expect(dot).not.toBeNull();
  });

  it("expanded: an idle-unread row in the regular list is styled orange '未读' (not gray idle)", async () => {
    // 用户痛点：展开后侧栏看不到未读高亮。bubble 在 expanded 时把 unread 滤掉，
    // 但下方常规列表应当替它接管 —— 把 idle-unread 行渲染成橙色（waiting）"未读"标签。
    localStorage.setItem("agentre.agentExpanded.agent:7", "1");
    // session.lastMessageAt=2000 > session.lastReadAt=0(默认) → 触发 unread。

    appMocks.ListChatAgents.mockResolvedValue({
      agents: [
        {
          activeCount: 0,
          attentionSessions: [],
          avatarColor: "agent-1",
          backendType: "builtin",
          chattable: true,
          id: 7,
          name: "Eng",
          pinned: false,
          recentCount: 1,
          sessions: [
            {
              id: 5,
              lastMessageAt: 2000,
              status: "idle",
              title: "Background done",
            },
          ],
        },
      ],
    });

    renderChatPage();

    const row = await screen.findByRole("button", { name: /Background done/ });
    // 应该带 "未读" 标签 + waiting 颜色 dot（橙色）。
    expect(row).toHaveTextContent("Unread");
    const dot = row.querySelector('[aria-label="waiting status"]');
    expect(dot).not.toBeNull();
  });

  it("expanded: unread sessions stay in the bubble (so they can pick up ⌘N chip) and don't duplicate into the regular list", async () => {
    localStorage.setItem("agentre.agentExpanded.agent:7", "1");
    // session 5 默认 lastReadAt=0 < lastMessageAt=2000 → unread；自统一改造后,
    // unread 在 expanded 态下也留在 bubble,以拿到 ⌘N 快捷键 chip;
    // 下方常规列表通过 attentionIds 去重,所以不会重复出现。

    appMocks.ListChatAgents.mockResolvedValue({
      agents: [
        {
          activeCount: 1,
          attentionSessions: [
            {
              id: 4,
              lastMessageAt: 3000,
              needsAttention: false,
              status: "running",
              title: "Running one",
            },
          ],
          avatarColor: "agent-1",
          backendType: "builtin",
          chattable: true,
          id: 7,
          name: "Eng",
          pinned: false,
          recentCount: 2,
          sessions: [
            {
              id: 4,
              lastMessageAt: 3000,
              status: "running",
              title: "Running one",
            },
            {
              id: 5,
              lastMessageAt: 2000,
              status: "idle",
              title: "Background done",
            },
          ],
        },
      ],
    });

    renderChatPage();

    // Wait for layout settled
    await screen.findByRole("button", { name: /Background done/ });

    // 展开态：running + unread 都留在 bubble
    const bubble = document.querySelector(
      '[data-slot="agent-attention-bubble"]',
    );
    expect(bubble).not.toBeNull();
    expect(bubble!).toHaveTextContent("Running one");
    expect(bubble!).toHaveTextContent("Background done");

    // 全页 unread 会话只渲染一行 SessionRow(在 bubble 里,不在下方常规列表)。
    expect(
      screen.getAllByRole("button", { name: /Background done/ }),
    ).toHaveLength(1);
  });

  it("does not duplicate an attention session in the expanded regular list", async () => {
    appMocks.ListChatAgents.mockResolvedValue({
      agents: [
        {
          activeCount: 1,
          attentionSessions: [
            {
              id: 1,
              lastMessageAt: 2000,
              needsAttention: false,
              status: "running",
              title: "Debug session",
            },
          ],
          avatarColor: "agent-1",
          backendType: "builtin",
          chattable: true,
          id: 7,
          name: "Eng",
          pinned: false,
          recentCount: 2,
          sessions: [
            {
              id: 1,
              lastMessageAt: 2000,
              status: "running",
              title: "Debug session",
            },
            {
              id: 2,
              lastMessageAt: 1000,
              status: "idle",
              title: "Other session",
            },
          ],
        },
      ],
    });

    renderChatPage();

    // 等 sidebar 渲染好
    await screen.findByRole("button", { name: /Other session/ });

    // "Debug session" 既在 attentionSessions 又在 sessions —— bubble 要展示一次，
    // 下方 5 行列表必须去重，所以整页只能找到一行带这个标题的 SessionRow。
    const debugRows = screen.getAllByRole("button", { name: /Debug session/ });
    expect(debugRows).toHaveLength(1);

    // sanity: 另一条非 attention 会话仍正常出现在下方列表
    expect(
      screen.getByRole("button", { name: /Other session/ }),
    ).toBeInTheDocument();
  });
});

describe("ChatPage sidebar — 新建会话按钮接入 chat-tabs", () => {
  // 回归：Tab 化重构后,sidebar 的「+ 新建会话」按 setSelectedChat(aid, 0) 走
  // 老 facade,而 selected-chat-store 在 sessionId===0 分支什么都不做,
  // 导致 ChatPanelHost (完全由 useChatTabsStore 驱动) 永远不会出新 tab。
  // 点 + 后必须在 useChatTabsStore 里出现一个 kind:"new" + agentId 正确的 tab。
  beforeEach(() => {
    localStorage.clear();
    resetTabsStore();
    runtimeMocks.handlers.clear();
    vi.clearAllMocks();
    appMocks.ListChatAgents.mockResolvedValue({
      agents: [
        {
          activeCount: 0,
          avatarColor: "agent-1",
          backendType: "builtin",
          chattable: true,
          id: 7,
          name: "Eng",
          pinned: false,
          recentCount: 0,
          sessions: [],
        },
      ],
    });
  });

  afterEach(() => {
    resetTabsStore();
    localStorage.clear();
  });

  it('clicking the + button opens a kind:"new" tab for that agent', async () => {
    renderChatPage();

    const plus = await screen.findByRole("button", { name: "New Eng session" });
    fireEvent.click(plus);

    await waitFor(() => {
      const { tabs, activeTabId } = useChatTabsStore.getState();
      expect(tabs).toHaveLength(1);
      expect(tabs[0].id).toBe(activeTabId);
      expect(tabs[0].meta).toMatchObject({ kind: "new", agentId: 7 });
    });
  });
});

describe("ChatPage sidebar — 群聊分区", () => {
  // E4: 左侧对话列表在「Agents」之上混排一个「Group Chats」分区。
  // 该分区只在有群时渲染;点击群行打开 / 激活一个 group tab。
  beforeEach(() => {
    localStorage.clear();
    resetTabsStore();
    useGroupListStore.getState().__reset();
    runtimeMocks.handlers.clear();
    vi.clearAllMocks();
    appMocks.ListChatAgents.mockResolvedValue({ agents: [] });
  });

  afterEach(() => {
    resetTabsStore();
    useGroupListStore.getState().__reset();
    localStorage.clear();
  });

  it("没有群时不渲染 Group Chats 分区标题", async () => {
    appMocks.GroupList.mockResolvedValue([]);
    renderChatPage();
    // 等 sidebar 拉过一次群列表
    await waitFor(() => {
      expect(appMocks.GroupList).toHaveBeenCalled();
    });
    expect(screen.queryByText("Group Chats")).not.toBeInTheDocument();
  });

  it("有群时渲染 Group Chats 分区 + 群行;点击群行打开 group tab", async () => {
    appMocks.GroupList.mockResolvedValue([
      {
        id: 9,
        title: "Release Squad",
        runStatus: "running",
        roundCount: 2,
        createtime: 0,
        updatetime: 0,
      },
    ]);

    renderChatPage();

    const row = await screen.findByRole("button", { name: /Release Squad/ });
    expect(screen.getByText("Group Chats")).toBeInTheDocument();

    fireEvent.click(row);

    await waitFor(() => {
      const { tabs, activeTabId } = useChatTabsStore.getState();
      expect(tabs).toHaveLength(1);
      expect(tabs[0].id).toBe(activeTabId);
      expect(tabs[0].meta).toMatchObject({
        kind: "group",
        groupId: 9,
        title: "Release Squad",
      });
    });
  });
});

describe("ChatPage sidebar — 混排筛选与顶部新建", () => {
  beforeEach(() => {
    localStorage.clear();
    resetTabsStore();
    useCommandPaletteStore.setState({ open: false, initialQuery: "" });
    useGroupListStore.getState().__reset();
    runtimeMocks.handlers.clear();
    vi.clearAllMocks();
    appMocks.GroupList.mockResolvedValue([
      {
        id: 9,
        title: "Release Squad",
        runStatus: "running",
        roundCount: 2,
        createtime: 0,
        updatetime: 0,
      },
    ]);
    appMocks.ListChatAgents.mockResolvedValue({
      agents: [
        {
          activeCount: 1,
          avatarColor: "agent-1",
          backendType: "builtin",
          chattable: true,
          id: 7,
          name: "Eng",
          pinned: false,
          recentCount: 2,
          sessions: [
            {
              id: 4,
              lastMessageAt: 3000,
              lastReadAt: 3000,
              needsAttention: false,
              status: "running",
              title: "Running one",
            },
            {
              id: 5,
              lastMessageAt: 2000,
              lastReadAt: 0,
              needsAttention: false,
              status: "idle",
              title: "Background done",
            },
          ],
        },
        {
          activeCount: 0,
          avatarColor: "agent-2",
          backendType: "builtin",
          chattable: true,
          id: 8,
          name: "Designer",
          pinned: false,
          recentCount: 1,
          sessions: [
            {
              id: 6,
              lastMessageAt: 1000,
              lastReadAt: 1000,
              needsAttention: false,
              status: "idle",
              title: "Visual pass",
            },
          ],
        },
      ],
    });
  });

  afterEach(() => {
    resetTabsStore();
    useCommandPaletteStore.setState({ open: false, initialQuery: "" });
    useGroupListStore.getState().__reset();
    localStorage.clear();
  });

  it("Given mixed groups and agents, When the sidebar filter changes, Then the list narrows by type and attention state", async () => {
    renderChatPage();

    expect(
      await screen.findByRole("button", { name: /Release Squad/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /Open Eng recent session/ }),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Filter sidebar" }));
    fireEvent.click(screen.getByRole("button", { name: "Group chats" }));

    expect(
      screen.getByRole("button", { name: /Release Squad/ }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /Open Eng recent session/ }),
    ).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Filter sidebar" }));
    fireEvent.click(screen.getByRole("button", { name: "Agents" }));

    expect(screen.queryByRole("button", { name: /Release Squad/ })).toBeNull();
    expect(
      screen.getByRole("button", { name: /Open Eng recent session/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /Open Designer recent session/ }),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Filter sidebar" }));
    fireEvent.click(screen.getByRole("button", { name: "Running" }));

    expect(
      screen.getByRole("button", { name: /Release Squad/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /Open Eng recent session/ }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /Open Designer recent session/ }),
    ).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Filter sidebar" }));
    fireEvent.click(
      within(screen.getByRole("dialog")).getByRole("button", {
        name: /Unread/,
      }),
    );

    expect(screen.queryByRole("button", { name: /Release Squad/ })).toBeNull();
    expect(
      screen.getByRole("button", { name: /Open Eng recent session/ }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /Open Designer recent session/ }),
    ).toBeNull();
  });

  it("Given the mixed sidebar, When the top + menu picks new agent chat, Then it opens the new-chat command palette seed", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    renderChatPage();

    await user.click(await screen.findByRole("button", { name: "New" }));
    await user.click(
      await screen.findByRole("menuitem", { name: "New agent chat" }),
    );

    expect(useCommandPaletteStore.getState()).toMatchObject({
      open: true,
      initialQuery: "> ",
    });
  });

  it("Given the mixed sidebar, When the top + menu picks new group, Then the new-group dialog opens", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    renderChatPage();

    await user.click(await screen.findByRole("button", { name: "New" }));
    await user.click(
      await screen.findByRole("menuitem", { name: "New group" }),
    );

    // 弹窗 footer 的「Create group」按钮出现 = 新建群聊弹窗已打开。
    expect(
      await screen.findByRole("button", { name: "Create group" }),
    ).toBeInTheDocument();
  });
});
