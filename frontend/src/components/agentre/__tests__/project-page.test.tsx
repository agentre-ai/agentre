import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type * as React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useChatAgentsStore } from "@/stores/chat-agents-store";
import { useChatTabsStore } from "@/stores/chat-tabs-store";
import { useSessionReadStore } from "@/stores/session-read-store";

const appMocks = vi.hoisted(() => ({
  ListChatAgents: vi.fn(),
  ProjectAddMember: vi.fn(),
  ProjectCreate: vi.fn(),
  ProjectDelete: vi.fn(),
  ProjectDetectGitRepo: vi.fn(),
  ProjectGet: vi.fn(),
  ProjectListSessions: vi.fn(),
  ProjectListTree: vi.fn(),
  ProjectLocationList: vi.fn(),
  ProjectRemoveMember: vi.fn(),
  ProjectReorder: vi.fn(),
  ProjectUpdate: vi.fn(),
  RemoteDeviceList: vi.fn(),
  SelectDirectory: vi.fn(),
}));

const dndMocks = vi.hoisted(() => ({
  onDragEnd: null as null | ((event: unknown) => void),
}));

type MockDndContextProps = {
  children: React.ReactNode;
  onDragEnd: (event: unknown) => void;
};

type MockSortableContextProps = {
  children: React.ReactNode;
};

vi.mock("../../../../wailsjs/go/app/App", () => appMocks);
vi.mock("@dnd-kit/core", () => ({
  DndContext: ({ children, onDragEnd }: MockDndContextProps) => {
    dndMocks.onDragEnd = onDragEnd;
    return children;
  },
  KeyboardSensor: vi.fn(),
  PointerSensor: vi.fn(),
  useSensor: vi.fn(() => ({})),
  useSensors: vi.fn(() => []),
}));
vi.mock("@dnd-kit/sortable", () => ({
  SortableContext: ({ children }: MockSortableContextProps) => children,
  sortableKeyboardCoordinates: vi.fn(),
  useSortable: vi.fn(() => ({
    attributes: {},
    isDragging: false,
    listeners: {},
    setActivatorNodeRef: vi.fn(),
    setNodeRef: vi.fn(),
    transform: null,
    transition: undefined,
  })),
  verticalListSortingStrategy: {},
}));

import { ProjectsPage } from "../project-page";
import { ProjectSettingsDrawer } from "../project-settings-drawer";

function renderProjectsPage() {
  return render(<ProjectsPage />);
}

// Radix DropdownMenu 在 jsdom 中需要关闭 pointerEvents 检查。
function setupUser() {
  return userEvent.setup({ pointerEventsCheck: 0 });
}

describe("ProjectsPage session read state", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    useSessionReadStore.setState({ overrides: new Map() });
    useChatAgentsStore.getState().__reset();
    useChatTabsStore.setState({ tabs: [], activeTabId: null });

    appMocks.ListChatAgents.mockResolvedValue({ agents: [] });
    appMocks.ProjectGet.mockResolvedValue({
      project: null,
      directMembers: [],
      inheritedMembers: [],
    });
    appMocks.ProjectLocationList.mockResolvedValue([]);
    appMocks.RemoteDeviceList.mockResolvedValue([]);
    appMocks.ProjectListTree.mockResolvedValue([
      {
        project: {
          color: "agent-1",
          icon: "folder",
          id: 1,
          name: "Agentre",
          parentID: 0,
          path: "/tmp/agentre",
        },
        children: [],
      },
    ]);
  });

  afterEach(() => {
    localStorage.clear();
  });

  it("uses server lastReadAt so a read project session stays read after restart", async () => {
    appMocks.ProjectListSessions.mockResolvedValue([
      {
        agentID: 7,
        agentStatus: "idle",
        id: 11,
        lastMessageAt: 2000,
        lastReadAt: 3000,
        needsAttention: false,
        title: "Read after restart",
      },
    ]);

    renderProjectsPage();

    await screen.findByRole("button", { name: /Read after restart/ });

    expect(screen.queryByText("未读")).not.toBeInTheDocument();
    expect(
      document.querySelector('[data-slot="agent-attention-bubble"]'),
    ).toBeNull();
  });

  it("uses the same optimistic read overlay as the chat page", async () => {
    useSessionReadStore.getState().markRead(11, 3000);
    appMocks.ProjectListSessions.mockResolvedValue([
      {
        agentID: 7,
        agentStatus: "idle",
        id: 11,
        lastMessageAt: 2000,
        lastReadAt: 0,
        needsAttention: false,
        title: "Optimistically read",
      },
    ]);

    renderProjectsPage();

    await screen.findByRole("button", { name: /Optimistically read/ });

    await waitFor(() => {
      expect(screen.queryByText("未读")).not.toBeInTheDocument();
    });
    expect(
      document.querySelector('[data-slot="agent-attention-bubble"]'),
    ).toBeNull();
  });

  it("still shows unread when lastMessageAt is newer than lastReadAt", async () => {
    appMocks.ProjectListSessions.mockResolvedValue([
      {
        agentID: 7,
        agentStatus: "idle",
        id: 11,
        lastMessageAt: 3000,
        lastReadAt: 2000,
        needsAttention: false,
        title: "Unread project session",
      },
    ]);

    renderProjectsPage();

    await screen.findByRole("button", { name: /Unread project session/ });

    const bubble = document.querySelector(
      '[data-slot="agent-attention-bubble"]',
    );
    expect(bubble).not.toBeNull();
    expect(bubble!).toHaveTextContent("Unread project session");
    expect(bubble!).toHaveTextContent("未读");
  });
});

describe("ProjectsPage collapsed parent attention rollup", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    useSessionReadStore.setState({ overrides: new Map() });
    useChatAgentsStore.getState().__reset();
    useChatTabsStore.setState({ tabs: [], activeTabId: null });

    appMocks.ListChatAgents.mockResolvedValue({ agents: [] });
    appMocks.ProjectGet.mockResolvedValue({
      project: null,
      directMembers: [],
      inheritedMembers: [],
    });
    appMocks.ProjectLocationList.mockResolvedValue([]);
    appMocks.RemoteDeviceList.mockResolvedValue([]);
    appMocks.ProjectListTree.mockResolvedValue([
      {
        project: {
          color: "agent-1",
          icon: "folder",
          id: 1,
          name: "Agentre",
          parentID: 0,
          path: "/tmp/agentre",
        },
        children: [
          {
            project: {
              color: "agent-2",
              icon: "folder",
              id: 2,
              name: "backend",
              parentID: 1,
              path: "/tmp/agentre/backend",
            },
            children: [],
          },
        ],
      },
    ]);
  });

  afterEach(() => {
    localStorage.clear();
  });

  it("Given a parent project is collapsed, When a child project has an unread session, Then the parent shows a clickable rollup bubble", async () => {
    localStorage.setItem("agentre.agentExpanded.project:1", "0");
    appMocks.ProjectListSessions.mockImplementation(
      async (projectID: number) =>
        projectID === 2
          ? [
              {
                agentID: 7,
                agentStatus: "idle",
                id: 22,
                lastMessageAt: 3000,
                lastReadAt: 1000,
                needsAttention: false,
                title: "Child unread session",
              },
            ]
          : [],
    );

    renderProjectsPage();

    const bubble = await waitFor(() => {
      const found = document.querySelector(
        '[data-slot="agent-attention-bubble"]',
      );
      expect(found).not.toBeNull();
      return found!;
    });
    expect(bubble).toHaveTextContent("Child unread session");
    expect(bubble).toHaveTextContent("未读");

    await setupUser().click(
      screen.getByRole("button", { name: /Child unread session/ }),
    );

    await waitFor(() => {
      const active = useChatTabsStore
        .getState()
        .tabs.find((t) => t.id === useChatTabsStore.getState().activeTabId);
      expect(active?.meta).toMatchObject({
        kind: "session",
        sessionId: 22,
      });
    });
  });

  it("Given a parent project is collapsed, When child projects need approval or are running, Then the parent reuses attention labels and active count", async () => {
    localStorage.setItem("agentre.agentExpanded.project:1", "0");
    appMocks.ProjectListSessions.mockImplementation(
      async (projectID: number) =>
        projectID === 2
          ? [
              {
                agentID: 7,
                agentStatus: "idle",
                id: 23,
                lastMessageAt: 4000,
                lastReadAt: 4000,
                needsAttention: true,
                title: "Child approval session",
              },
              {
                agentID: 7,
                agentStatus: "running",
                id: 24,
                lastMessageAt: 5000,
                lastReadAt: 5000,
                needsAttention: false,
                title: "Child running session",
              },
            ]
          : [],
    );

    renderProjectsPage();

    const bubble = await waitFor(() => {
      const found = document.querySelector(
        '[data-slot="agent-attention-bubble"]',
      );
      expect(found).not.toBeNull();
      return found!;
    });
    expect(bubble).toHaveTextContent("Child approval session");
    expect(bubble).toHaveTextContent("审批");
    expect(bubble).toHaveTextContent("Child running session");

    const parentButton = screen
      .getAllByRole("button", { name: /Agentre/ })
      .find((button) => button.getAttribute("aria-expanded") !== null);
    expect(parentButton).toBeTruthy();
    expect(parentButton).toHaveTextContent("2");
  });
});

describe("ProjectsPage nesting visuals (B1)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    useSessionReadStore.setState({ overrides: new Map() });
    useChatAgentsStore.getState().__reset();
    useChatTabsStore.setState({ tabs: [], activeTabId: null });

    appMocks.ListChatAgents.mockResolvedValue({ agents: [] });
    appMocks.ProjectGet.mockResolvedValue({
      project: null,
      directMembers: [],
      inheritedMembers: [],
    });
    appMocks.ProjectLocationList.mockResolvedValue([]);
    appMocks.RemoteDeviceList.mockResolvedValue([]);
    appMocks.ProjectListSessions.mockResolvedValue([]);
    appMocks.ProjectListTree.mockResolvedValue([
      {
        project: {
          color: "agent-1",
          icon: "folder",
          id: 1,
          name: "Agentre",
          parentID: 0,
          path: "/tmp/agentre",
        },
        children: [
          {
            project: {
              color: "agent-2",
              icon: "folder",
              id: 2,
              name: "backend",
              parentID: 1,
              path: "/tmp/agentre/backend",
            },
            children: [],
          },
        ],
      },
    ]);
  });

  afterEach(() => {
    localStorage.clear();
  });

  it("does not render the legacy left border on nested sub-projects", async () => {
    renderProjectsPage();
    const subLabel = await screen.findByText("backend");
    for (let el: HTMLElement | null = subLabel; el; el = el.parentElement) {
      expect(el.className).not.toMatch(/\bborder-l\b/);
    }
  });

  it("renders sub-project header as uppercase mono section label", async () => {
    renderProjectsPage();
    const label = await screen.findByText("backend");
    expect(label.className).toMatch(/font-mono/);
    expect(label.className).toMatch(/uppercase/);
    expect(label.className).toMatch(/text-muted-foreground/);
  });

  it("keeps the root project name rendered in sans (no uppercase)", async () => {
    renderProjectsPage();
    const root = await screen.findByText("Agentre");
    expect(root.className).not.toMatch(/font-mono/);
    expect(root.className).not.toMatch(/uppercase/);
  });
});

describe("ProjectsPage shell layout", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    useSessionReadStore.setState({ overrides: new Map() });
    useChatAgentsStore.getState().__reset();
    useChatTabsStore.setState({ tabs: [], activeTabId: null });

    appMocks.ListChatAgents.mockResolvedValue({ agents: [] });
    appMocks.ProjectGet.mockResolvedValue({
      project: null,
      directMembers: [],
      inheritedMembers: [],
    });
    appMocks.ProjectLocationList.mockResolvedValue([]);
    appMocks.RemoteDeviceList.mockResolvedValue([]);
    appMocks.ProjectListSessions.mockResolvedValue([]);
    appMocks.ProjectListTree.mockResolvedValue([]);
  });

  afterEach(() => {
    localStorage.clear();
  });

  it("renders the project sidebar as the outlet root so the chat pane sits immediately beside it", async () => {
    const { container } = renderProjectsPage();

    const sidebar = await screen.findByRole("complementary", {
      name: "项目列表",
    });

    expect(sidebar.parentElement).toBe(container);
    expect(sidebar.parentElement).not.toHaveClass("flex-1");
  });
});

describe("ProjectsPage project drag reorder", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    dndMocks.onDragEnd = null;
    localStorage.clear();
    useSessionReadStore.setState({ overrides: new Map() });
    useChatAgentsStore.getState().__reset();
    useChatTabsStore.setState({ tabs: [], activeTabId: null });

    appMocks.ListChatAgents.mockResolvedValue({ agents: [] });
    appMocks.ProjectGet.mockResolvedValue({
      project: null,
      directMembers: [],
      inheritedMembers: [],
    });
    appMocks.ProjectListSessions.mockResolvedValue([]);
    appMocks.ProjectLocationList.mockResolvedValue([]);
    appMocks.ProjectReorder.mockResolvedValue(undefined);
    appMocks.RemoteDeviceList.mockResolvedValue([]);
  });

  afterEach(() => {
    localStorage.clear();
  });

  it("Given root projects, When one root is dropped before another, Then it persists the new root order", async () => {
    appMocks.ProjectListTree.mockResolvedValue([
      {
        project: { id: 1, name: "Alpha", parentID: 0, path: "/tmp/a" },
        children: [],
      },
      {
        project: { id: 2, name: "Beta", parentID: 0, path: "/tmp/b" },
        children: [],
      },
      {
        project: { id: 3, name: "Gamma", parentID: 0, path: "/tmp/c" },
        children: [],
      },
    ]);

    renderProjectsPage();

    await screen.findByText("Gamma");
    dndMocks.onDragEnd?.({
      active: { id: "project-3" },
      over: { id: "project-1" },
    });

    await waitFor(() => {
      expect(appMocks.ProjectReorder).toHaveBeenCalledWith({
        parentID: 0,
        orderedIDs: [3, 1, 2],
      });
    });
  });

  it("Given sub-projects, When one child is dropped before a sibling, Then it persists the child order under its parent", async () => {
    appMocks.ProjectListTree.mockResolvedValue([
      {
        project: { id: 1, name: "Root", parentID: 0, path: "/tmp/root" },
        children: [
          {
            project: { id: 2, name: "Child A", parentID: 1, path: "/tmp/a" },
            children: [],
          },
          {
            project: { id: 3, name: "Child B", parentID: 1, path: "/tmp/b" },
            children: [],
          },
        ],
      },
    ]);

    renderProjectsPage();

    await screen.findByText("Child B");
    dndMocks.onDragEnd?.({
      active: { id: "project-3" },
      over: { id: "project-2" },
    });

    await waitFor(() => {
      expect(appMocks.ProjectReorder).toHaveBeenCalledWith({
        parentID: 1,
        orderedIDs: [3, 2],
      });
    });
  });

  it("Given a project row, Then no explicit grip handle is rendered (the row itself is the drag activator)", async () => {
    appMocks.ProjectListTree.mockResolvedValue([
      {
        project: { id: 1, name: "Alpha", parentID: 0, path: "/tmp/a" },
        children: [],
      },
    ]);

    renderProjectsPage();

    await screen.findByText("Alpha");
    expect(
      screen.queryByRole("button", { name: /Alpha 拖拽排序/ }),
    ).not.toBeInTheDocument();
  });

  it("Given a search filter, When a drag end event fires, Then it does not persist a partial visible order", async () => {
    const user = setupUser();
    appMocks.ProjectListTree.mockResolvedValue([
      {
        project: { id: 1, name: "Alpha", parentID: 0, path: "/tmp/a" },
        children: [],
      },
      {
        project: { id: 2, name: "Beta", parentID: 0, path: "/tmp/b" },
        children: [],
      },
    ]);

    renderProjectsPage();
    await user.type(await screen.findByLabelText("搜索项目 / 会话"), "Al");
    dndMocks.onDragEnd?.({
      active: { id: "project-2" },
      over: { id: "project-1" },
    });

    expect(appMocks.ProjectReorder).not.toHaveBeenCalled();
  });

  it("Given reorder persistence fails, When a project is dropped, Then the visible order rolls back", async () => {
    appMocks.ProjectReorder.mockRejectedValueOnce(new Error("boom"));
    appMocks.ProjectListTree.mockResolvedValue([
      {
        project: { id: 1, name: "Alpha", parentID: 0, path: "/tmp/a" },
        children: [],
      },
      {
        project: { id: 2, name: "Beta", parentID: 0, path: "/tmp/b" },
        children: [],
      },
    ]);

    renderProjectsPage();

    await screen.findByText("Beta");
    dndMocks.onDragEnd?.({
      active: { id: "project-2" },
      over: { id: "project-1" },
    });

    await waitFor(() => {
      expect(appMocks.ProjectReorder).toHaveBeenCalled();
    });
    await waitFor(() => {
      const labels = screen
        .getAllByText(/Alpha|Beta/)
        .map((el) => el.textContent);
      expect(labels).toEqual(["Alpha", "Beta"]);
    });
  });
});

describe("ProjectsPage project new-session menu", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    useSessionReadStore.setState({ overrides: new Map() });
    useChatAgentsStore.getState().__reset();
    useChatTabsStore.setState({ tabs: [], activeTabId: null });

    appMocks.ListChatAgents.mockResolvedValue({ agents: [] });
    appMocks.ProjectListSessions.mockResolvedValue([]);
    appMocks.ProjectListTree.mockResolvedValue([
      {
        project: {
          color: "agent-1",
          icon: "folder",
          id: 1,
          name: "Agentre",
          parentID: 0,
          path: "/tmp/agentre",
        },
        children: [],
      },
    ]);
    appMocks.ProjectLocationList.mockResolvedValue([]);
    appMocks.RemoteDeviceList.mockResolvedValue([]);
  });

  afterEach(() => {
    localStorage.clear();
  });

  it("renders ProjectGet member names even when the chat-agent snapshot is empty", async () => {
    const user = setupUser();
    appMocks.ProjectGet.mockResolvedValue({
      project: { id: 1, name: "Agentre" },
      directMembers: [
        {
          agentID: 5,
          agentName: "Builder",
          avatarColor: "agent-2",
          avatarIcon: "hammer",
          inherited: false,
        },
        {
          agentID: 6,
          agentName: "Reviewer",
          avatarColor: "agent-3",
          inherited: false,
        },
      ],
      inheritedMembers: [],
    });

    renderProjectsPage();

    await user.click(
      await screen.findByRole("button", { name: "Agentre 新建会话" }),
    );

    expect(await screen.findByText("Builder")).toBeInTheDocument();
    expect(screen.queryByText(/还没添加成员/)).not.toBeInTheDocument();
  });

  it("opens a project-scoped new-session tab with the selected member id", async () => {
    const user = setupUser();
    appMocks.ProjectGet.mockResolvedValue({
      project: { id: 1, name: "Agentre" },
      directMembers: [
        {
          agentID: 5,
          agentName: "Builder",
          avatarColor: "agent-2",
          inherited: false,
        },
        {
          agentID: 6,
          agentName: "Reviewer",
          avatarColor: "agent-3",
          inherited: false,
        },
      ],
      inheritedMembers: [],
    });

    renderProjectsPage();

    await user.click(
      await screen.findByRole("button", { name: "Agentre 新建会话" }),
    );
    await user.click(await screen.findByText("Builder"));

    await waitFor(() => {
      const active = useChatTabsStore
        .getState()
        .tabs.find((t) => t.id === useChatTabsStore.getState().activeTabId);
      expect(active?.meta).toMatchObject({
        kind: "new",
        projectId: 1,
        agentId: 5,
        workMode: "",
      });
    });
  });

  it("Given a project has exactly one bound agent, When clicking new session, Then it opens the chat directly without asking the user to pick", async () => {
    const user = setupUser();
    appMocks.ProjectGet.mockResolvedValue({
      project: { id: 1, name: "Agentre" },
      directMembers: [
        {
          agentID: 5,
          agentName: "Builder",
          avatarColor: "agent-2",
          inherited: false,
        },
      ],
      inheritedMembers: [],
    });

    renderProjectsPage();

    await user.click(
      await screen.findByRole("button", { name: "Agentre 新建会话" }),
    );

    await waitFor(() => {
      const active = useChatTabsStore
        .getState()
        .tabs.find((t) => t.id === useChatTabsStore.getState().activeTabId);
      expect(active?.meta).toMatchObject({
        kind: "new",
        projectId: 1,
        agentId: 5,
        workMode: "",
      });
    });
    expect(screen.queryByText("选一个 Agent")).not.toBeInTheDocument();
    expect(screen.queryByText("Builder")).not.toBeInTheDocument();
  });

  it("Given a user picks an agent from the + menu, Then Radix does not steal focus back to the + trigger (so the new tab's editor can claim it)", async () => {
    // 回归: 项目页新建会话时,输入框「获取到了焦点又丢失了」——
    // Radix DropdownMenu 默认的 onCloseAutoFocus 在菜单关闭时把焦点还给
    // trigger,抢走 ChatPanelHost setTimeout(0) 给编辑器的 focus。
    // 修复后 onCloseAutoFocus 被 preventDefault,trigger 不再夺焦。
    const user = setupUser();
    appMocks.ProjectGet.mockResolvedValue({
      project: { id: 1, name: "Agentre" },
      directMembers: [
        {
          agentID: 5,
          agentName: "Builder",
          avatarColor: "agent-2",
          inherited: false,
        },
        {
          agentID: 6,
          agentName: "Reviewer",
          avatarColor: "agent-3",
          inherited: false,
        },
      ],
      inheritedMembers: [],
    });

    renderProjectsPage();

    const trigger = await screen.findByRole("button", {
      name: "Agentre 新建会话",
    });
    await user.click(trigger);
    await user.click(await screen.findByText("Builder"));

    await waitFor(() => {
      expect(document.activeElement).not.toBe(trigger);
    });
  });

  it("refetches members on reopen instead of reusing a stale empty menu", async () => {
    const user = setupUser();
    appMocks.ProjectGet.mockResolvedValueOnce({
      project: { id: 1, name: "Agentre" },
      directMembers: [],
      inheritedMembers: [],
    }).mockResolvedValueOnce({
      project: { id: 1, name: "Agentre" },
      directMembers: [
        {
          agentID: 6,
          agentName: "Reviewer",
          avatarColor: "agent-3",
          inherited: false,
        },
        {
          agentID: 7,
          agentName: "Auditor",
          avatarColor: "agent-4",
          inherited: false,
        },
      ],
      inheritedMembers: [],
    });

    renderProjectsPage();

    const trigger = await screen.findByRole("button", {
      name: "Agentre 新建会话",
    });
    await user.click(trigger);
    expect(await screen.findByText(/还没添加成员/)).toBeInTheDocument();

    await user.keyboard("{Escape}");
    await waitFor(() => {
      expect(screen.queryByText(/还没添加成员/)).not.toBeInTheDocument();
    });
    await user.click(trigger);

    expect(await screen.findByText("Reviewer")).toBeInTheDocument();
    expect(appMocks.ProjectGet).toHaveBeenCalledTimes(2);
  });
});

describe("ProjectsPage active tab anchor", () => {
  // 在 chat-tabs-store 里塞一个 active session tab，模拟外部（chat 页 / tab strip /
  // 命令面板）切换了当前 tab —— project-page 不会自己触发 selectOnTab。
  function selectSessionTab(sessionId: number) {
    const tab = {
      id: `seed-tab-${sessionId}`,
      meta: { kind: "session" as const, sessionId },
      isPreview: false,
      isPinned: false,
      pinAt: 0,
      openedAt: 0,
    };
    useChatTabsStore.setState({ tabs: [tab], activeTabId: tab.id });
  }

  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    useSessionReadStore.setState({ overrides: new Map() });
    useChatAgentsStore.getState().__reset();
    useChatTabsStore.setState({ tabs: [], activeTabId: null });

    appMocks.ListChatAgents.mockResolvedValue({ agents: [] });
    appMocks.ProjectGet.mockResolvedValue({
      project: null,
      directMembers: [],
      inheritedMembers: [],
    });
    appMocks.ProjectLocationList.mockResolvedValue([]);
    appMocks.RemoteDeviceList.mockResolvedValue([]);
    appMocks.ProjectListTree.mockResolvedValue([
      {
        project: {
          color: "agent-1",
          icon: "folder",
          id: 1,
          name: "Agentre",
          parentID: 0,
          path: "/tmp/agentre",
        },
        children: [],
      },
    ]);
  });

  afterEach(() => {
    localStorage.clear();
  });

  it("Given an active chat tab whose session is outside the project Top 5, Then the active session is anchored into the project sidebar", async () => {
    // 6 条会话, lastMessageAt 递减; idle-anchor 是最老的一条, 不在 Top 5 之内。
    appMocks.ProjectListSessions.mockResolvedValue([
      buildSession({ id: 101, title: "Session A", lastMessageAt: 6000 }),
      buildSession({ id: 102, title: "Session B", lastMessageAt: 5000 }),
      buildSession({ id: 103, title: "Session C", lastMessageAt: 4000 }),
      buildSession({ id: 104, title: "Session D", lastMessageAt: 3000 }),
      buildSession({ id: 105, title: "Session E", lastMessageAt: 2000 }),
      buildSession({ id: 106, title: "Idle Anchor", lastMessageAt: 1000 }),
    ]);
    selectSessionTab(106);

    renderProjectsPage();

    const anchorRow = await screen.findByRole("button", {
      name: /Idle Anchor/,
    });
    expect(anchorRow).toHaveAttribute("aria-current", "true");
  });

  it("Given an active session that belongs to another project, Then this project's sidebar does not surface that foreign session", async () => {
    appMocks.ProjectListSessions.mockResolvedValue([
      buildSession({ id: 201, title: "Local A", lastMessageAt: 2000 }),
      buildSession({ id: 202, title: "Local B", lastMessageAt: 1000 }),
    ]);
    selectSessionTab(9999); // 不属于该 project 的 sessionId

    renderProjectsPage();

    await screen.findByRole("button", { name: /Local A/ });
    // 9999 不在 ownSessions 里, 不应被锚定到列表
    expect(
      screen.queryByRole("button", { name: /9999/ }),
    ).not.toBeInTheDocument();
    // Local A 也不应被错误地标记为 selected
    const localA = screen.getByRole("button", { name: /Local A/ });
    expect(localA).not.toHaveAttribute("aria-current", "true");
  });

  it("Given the active tab is a 'new' (unsaved) tab, Then no extra anchor row is added to the project sidebar", async () => {
    appMocks.ProjectListSessions.mockResolvedValue([
      buildSession({ id: 301, title: "Only Session", lastMessageAt: 1000 }),
    ]);
    useChatTabsStore.setState({
      tabs: [
        {
          id: "seed-new-tab",
          meta: { kind: "new", projectId: 1, agentId: 5, workMode: "" },
          isPreview: false,
          isPinned: false,
          pinAt: 0,
          openedAt: 0,
        },
      ],
      activeTabId: "seed-new-tab",
    });

    renderProjectsPage();

    await screen.findByRole("button", { name: /Only Session/ });
    // 只应该有这一条会话按钮, 不应该出现空标题的占位 row
    const sessionButtons = screen
      .getAllByRole("button")
      .filter((btn) => btn.getAttribute("aria-current") === "true");
    expect(sessionButtons).toHaveLength(0);
  });

  it("Given the active tab is in Top 5, Then the row is highlighted via aria-current without external selection clicks", async () => {
    appMocks.ProjectListSessions.mockResolvedValue([
      buildSession({ id: 401, title: "Top One", lastMessageAt: 5000 }),
      buildSession({ id: 402, title: "Top Two", lastMessageAt: 4000 }),
    ]);
    selectSessionTab(402);

    renderProjectsPage();

    const topTwo = await screen.findByRole("button", { name: /Top Two/ });
    expect(topTwo).toHaveAttribute("aria-current", "true");
    const topOne = screen.getByRole("button", { name: /Top One/ });
    expect(topOne).not.toHaveAttribute("aria-current", "true");
  });
});

function buildSession({
  id,
  title,
  lastMessageAt,
}: {
  id: number;
  title: string;
  lastMessageAt: number;
}) {
  return {
    agentID: 7,
    agentStatus: "idle",
    id,
    lastMessageAt,
    lastReadAt: lastMessageAt, // 默认已读, 不让 unread 把行抢去 attention bubble
    needsAttention: false,
    title,
  };
}

describe("ProjectSettingsDrawer members", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    useChatAgentsStore.getState().__reset();
    appMocks.ListChatAgents.mockResolvedValue({ agents: [] });
    appMocks.ProjectLocationList.mockResolvedValue([]);
    appMocks.RemoteDeviceList.mockResolvedValue([]);
  });

  afterEach(() => {
    localStorage.clear();
  });

  it("uses ProjectGet member display names before falling back to Agent #id", async () => {
    const user = setupUser();
    appMocks.ProjectGet.mockResolvedValue({
      project: {
        color: "agent-1",
        description: "",
        icon: "folder",
        id: 1,
        name: "Agentre",
        path: "/tmp/agentre",
      },
      directMembers: [
        {
          agentID: 5,
          agentName: "Builder",
          avatarColor: "agent-2",
          avatarIcon: "hammer",
          inherited: false,
        },
      ],
      inheritedMembers: [],
    });

    render(
      <ProjectSettingsDrawer
        projectID={1}
        onClose={() => {}}
        onChanged={() => {}}
        onDeleted={() => {}}
      />,
    );

    await user.click(await screen.findByRole("button", { name: "成员" }));

    expect(await screen.findByText("Builder")).toBeInTheDocument();
    expect(screen.queryByText("Agent #5")).not.toBeInTheDocument();
  });
});
