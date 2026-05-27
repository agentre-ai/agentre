import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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
  ProjectUpdate: vi.fn(),
  RemoteDeviceList: vi.fn(),
  SelectDirectory: vi.fn(),
}));

vi.mock("../../../../wailsjs/go/app/App", () => appMocks);

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
