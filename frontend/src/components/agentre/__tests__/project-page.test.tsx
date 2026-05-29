import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type * as React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../../wailsjs/runtime/runtime", () => ({
  EventsOff: vi.fn(),
  EventsOn: vi.fn(),
}));

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

import { ProjectsPage, NewTerminalSubMenu } from "../project-page";
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

    expect(screen.queryByText("Unread")).not.toBeInTheDocument();
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
      expect(screen.queryByText("Unread")).not.toBeInTheDocument();
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
    expect(bubble!).toHaveTextContent("Unread");
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
    expect(bubble).toHaveTextContent("Unread");

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
    expect(bubble).toHaveTextContent("Approval");
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
      name: "Project list",
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
    await user.type(
      await screen.findByLabelText("Search projects / sessions"),
      "Al",
    );
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
      await screen.findByRole("button", { name: "New session in Agentre" }),
    );

    expect(await screen.findByText("Builder")).toBeInTheDocument();
    expect(screen.queryByText(/No members yet/)).not.toBeInTheDocument();
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
      await screen.findByRole("button", { name: "New session in Agentre" }),
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
      await screen.findByRole("button", { name: "New session in Agentre" }),
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
    expect(screen.queryByText("Choose an Agent")).not.toBeInTheDocument();
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
      name: "New session in Agentre",
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
      name: "New session in Agentre",
    });
    await user.click(trigger);
    expect(await screen.findByText(/No members yet/)).toBeInTheDocument();

    await user.keyboard("{Escape}");
    await waitFor(() => {
      expect(screen.queryByText(/No members yet/)).not.toBeInTheDocument();
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

  it("Given a collapsed parent project, When the active tab is an idle child-project session, Then the parent rollup shows the active session", async () => {
    localStorage.setItem("agentre.agentExpanded.project:1", "0");
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
    appMocks.ProjectListSessions.mockImplementation(
      async (projectID: number) =>
        projectID === 2
          ? [
              buildSession({
                id: 502,
                title: "Child active idle",
                lastMessageAt: 5000,
              }),
            ]
          : [],
    );
    selectSessionTab(502);

    renderProjectsPage();

    const activeChild = await screen.findByRole("button", {
      name: /Child active idle/,
    });
    expect(activeChild).toHaveAttribute("aria-current", "true");
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

// NewTerminalSubMenu 组件级测试（Task 10）
// 策略：将组件包在 DropdownMenu(open) + DropdownMenuContent 里以满足 Radix 上下文要求，
// hover DropdownMenuSubTrigger 展开子内容（Radix sub 在 hover 时 open）；
// 用 pointerEventsCheck:0 绕开 jsdom 的 pointer-events:none 检查。
import {
  DropdownMenu,
  DropdownMenuContent,
} from "@/components/ui/dropdown-menu";

function SubMenuHarness({
  projectID,
  onPick,
}: {
  projectID: number;
  onPick: (deviceID: string, deviceName?: string) => void;
}) {
  return (
    <DropdownMenu open>
      <DropdownMenuContent>
        <NewTerminalSubMenu projectID={projectID} onPick={onPick} />
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

describe("NewTerminalSubMenu", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    appMocks.ProjectLocationList.mockResolvedValue([]);
    appMocks.RemoteDeviceList.mockResolvedValue([]);
  });

  function renderSubMenu(onPick = vi.fn()) {
    return {
      onPick,
      ...render(<SubMenuHarness projectID={1} onPick={onPick} />),
    };
  }

  // helper: hover SubTrigger to open the submenu, then wait for the sub content portal
  async function openSub(user: ReturnType<typeof userEvent.setup>) {
    const trigger = await screen.findByText("New Terminal");
    await user.hover(trigger);
  }

  it("Given the submenu trigger is rendered, Then it shows '新建终端' text", async () => {
    renderSubMenu();
    expect(await screen.findByText("New Terminal")).toBeInTheDocument();
  });

  it("Given the submenu is opened, Then it shows a 本地 item", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    renderSubMenu();

    await openSub(user);

    expect(await screen.findByText("Local")).toBeInTheDocument();
  });

  it("Given 本地 item is clicked, Then onPick is called with empty deviceID", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const { onPick } = renderSubMenu();

    await openSub(user);
    // fireEvent 直接触发 Radix DropdownMenuItem onSelect（不产生 pointer-leave 关闭菜单）
    fireEvent.click(await screen.findByText("Local"));

    expect(onPick).toHaveBeenCalledWith("", undefined);
  });

  it("Given an online device with a configured location, When the item is clicked, Then onPick fires with correct deviceID and name", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    appMocks.RemoteDeviceList.mockResolvedValue([
      { id: 42, name: "MacMini", online: true },
    ]);
    appMocks.ProjectLocationList.mockResolvedValue([
      {
        id: 1,
        projectId: 1,
        deviceId: "42",
        path: "/tmp/proj",
        deviceName: "MacMini",
        online: true,
      },
    ]);

    const { onPick } = renderSubMenu();

    await openSub(user);
    const item = await screen.findByText("MacMini");
    expect(item.closest("[data-disabled]")).toBeNull();

    fireEvent.click(item);

    expect(onPick).toHaveBeenCalledWith("42", "MacMini");
  });

  it("Given an online device without a configured location, Then the item is rendered disabled", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    appMocks.RemoteDeviceList.mockResolvedValue([
      { id: 7, name: "NAS", online: true },
    ]);
    appMocks.ProjectLocationList.mockResolvedValue([]);

    renderSubMenu();

    await openSub(user);
    const item = await screen.findByText(/NAS/);
    // DropdownMenuItem with disabled=true 携带 data-disabled="true"
    expect(item.closest("[data-disabled]")).not.toBeNull();
  });

  it("Given an offline device, Then the item is rendered disabled and shows 离线 label", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    appMocks.RemoteDeviceList.mockResolvedValue([
      { id: 9, name: "OfflineBox", online: false },
    ]);
    appMocks.ProjectLocationList.mockResolvedValue([
      {
        id: 2,
        projectId: 1,
        deviceId: "9",
        path: "/tmp/proj",
        deviceName: "OfflineBox",
        online: false,
      },
    ]);

    renderSubMenu();

    await openSub(user);
    const item = await screen.findByText(/OfflineBox/);
    expect(item).toHaveTextContent("offline");
    expect(item.closest("[data-disabled]")).not.toBeNull();
  });
});

// 端到端守住「菜单点击 → 真的多一个 terminal tab」的整条路由:
// ProjectCard 更多操作菜单 → 新建终端 子菜单 → 本地 → onSelect({kind:"open-terminal"})
// → selectOnTab → openTerminal → chat-tabs-store。SubMenuHarness 那组只测到 onPick,
// 这一组把 selectOnTab 的 open-terminal 分支也接上。
describe("ProjectsPage 新建终端 end-to-end routing", () => {
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

  it("Given the project menu, When 新建终端 → 本地 is clicked, Then a local terminal tab is opened and activated", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    renderProjectsPage();

    // 更多操作 → 新建终端 子菜单 → 本地
    await user.click(
      await screen.findByRole("button", { name: "More actions for Agentre" }),
    );
    await user.hover(await screen.findByText("New Terminal"));
    // fireEvent.click 触发 Radix item 的 onSelect（userEvent.click 的 pointer-leave 会先关菜单）
    fireEvent.click(await screen.findByText("Local"));

    await waitFor(() => {
      const state = useChatTabsStore.getState();
      const active = state.tabs.find((t) => t.id === state.activeTabId);
      expect(active?.meta).toMatchObject({
        kind: "terminal",
        projectId: 1,
        deviceId: "",
      });
    });
  });
});

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

    await user.click(await screen.findByRole("button", { name: "Members" }));

    expect(await screen.findByText("Builder")).toBeInTheDocument();
    expect(screen.queryByText("Agent #5")).not.toBeInTheDocument();
  });
});
