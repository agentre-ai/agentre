import { act, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { chat_svc } from "../../../../wailsjs/go/models";
import { useChatAgentsStore } from "@/stores/chat-agents-store";
import { useCommandPaletteStore } from "@/stores/command-palette-store";
import { useNewChatContextStore } from "@/stores/new-chat-context-store";

const appMocks = vi.hoisted(() => ({
  ListChatAgents: vi.fn(),
  ProjectGet: vi.fn(),
  ProjectLocationList: vi.fn(),
  ProjectListTree: vi.fn(),
}));

vi.mock("../../../../wailsjs/go/app/App", () => appMocks);

import { CommandPalette } from "./command-palette";

function mkAgent(
  over: Partial<chat_svc.ChatAgentItem> = {},
): chat_svc.ChatAgentItem {
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
  } as chat_svc.ChatAgentItem;
}

// 默认 /projects —— 多数 ContextBar/Tab/上下文相关测试都在
// 项目路由下成立。需要测自由会话 source 的用例显式传 "/chat"。
function renderHarness(initialPath = "/projects") {
  return render(
    <MemoryRouter initialEntries={[initialPath]}>
      <CommandPalette />
    </MemoryRouter>,
  );
}

async function flush() {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

beforeEach(() => {
  appMocks.ListChatAgents.mockReset();
  appMocks.ProjectGet.mockReset();
  appMocks.ProjectLocationList.mockReset();
  appMocks.ProjectListTree.mockReset();
  appMocks.ProjectGet.mockResolvedValue({
    project: null,
    directMembers: [],
    inheritedMembers: [],
  });
  appMocks.ProjectLocationList.mockResolvedValue([]);
  appMocks.ProjectListTree.mockResolvedValue([]);
  useChatAgentsStore.getState().__reset();
  useCommandPaletteStore.setState({ open: false, initialQuery: "" });
  useNewChatContextStore.getState().clear();
  localStorage.clear();
});

afterEach(() => {
  useCommandPaletteStore.setState({ open: false, initialQuery: "" });
  useNewChatContextStore.getState().clear();
});

describe("CommandPalette — ⌘N opens command mode and lists agents (BDD)", () => {
  it("Given /chat route + ⌘N seeds '> ', When palette opens, Then 命令 chip shows and every chattable agent renders as 'New chat with <name>'", async () => {
    appMocks.ListChatAgents.mockResolvedValue({
      agents: [
        mkAgent({ id: 1, name: "CEO 助手" }),
        mkAgent({ id: 2, name: "工程师" }),
        mkAgent({
          id: 3,
          name: "未绑后端",
          chattable: false,
          chattableHint: "请在组织页绑定后端",
        }),
      ],
    });

    renderHarness("/chat");
    await act(async () => {
      useCommandPaletteStore.getState().openWith("> ");
    });
    await flush();

    expect(screen.getByLabelText("Command mode")).toBeTruthy();
    expect(screen.getByText("CEO 助手")).toBeTruthy();
    expect(screen.getByText("工程师")).toBeTruthy();
    expect(screen.queryByText("未绑后端")).toBeNull();

    // /chat 路由：newChatSource 激活 → "New chat with"
    const labels = screen.getAllByText("New chat with");
    expect(labels.length).toBeGreaterThanOrEqual(2);
    // 同时 newProjectChatSource 未激活
    expect(screen.queryByText("New project chat with")).toBeNull();
  });

  it("ContextBar shows 无项目 when no project context is set (on /projects route)", async () => {
    appMocks.ListChatAgents.mockResolvedValue({
      agents: [mkAgent({ id: 1, name: "CEO 助手" })],
    });
    renderHarness("/projects");
    await act(async () => {
      useCommandPaletteStore.getState().openWith("> ");
    });
    await flush();

    expect(screen.getByText(/No project/)).toBeTruthy();
  });
});

describe("CommandPalette — ContextBar 项目 chip 可点击切换 (BDD)", () => {
  function mkProject(
    over: Partial<{
      id: number;
      name: string;
    }> = {},
  ) {
    return {
      id: 1,
      parentID: 0,
      name: "项目 A",
      icon: "",
      color: "",
      description: "",
      path: "",
      isGitRepo: false,
      createtime: 0,
      updatetime: 0,
      ...over,
    };
  }

  it("Given projects in tree, When clicking the project chip, Then the popover lists 无项目 + each project", async () => {
    appMocks.ListChatAgents.mockResolvedValue({ agents: [] });
    appMocks.ProjectListTree.mockResolvedValue([
      { project: mkProject({ id: 10, name: "后端重构" }), children: [] },
      { project: mkProject({ id: 11, name: "前端 UI" }), children: [] },
    ]);
    renderHarness();

    await act(async () => {
      useCommandPaletteStore.getState().openWith("> ");
    });
    await flush();

    const chip = screen.getByLabelText("Switch project context");
    await act(async () => {
      fireEvent.click(chip);
    });
    await flush();

    // chip 本身也叫"无项目"，popover 里会再出现一次 → 用 getAllByText
    expect(screen.getAllByText("No project").length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText("后端重构")).toBeTruthy();
    expect(screen.getByText("前端 UI")).toBeTruthy();
  });

  it("Selecting a project writes context store + closes popover", async () => {
    appMocks.ListChatAgents.mockResolvedValue({ agents: [] });
    appMocks.ProjectListTree.mockResolvedValue([
      {
        project: mkProject({
          id: 42,
          name: "后端重构",
        }),
        children: [],
      },
    ]);
    renderHarness();
    await act(async () => {
      useCommandPaletteStore.getState().openWith("> ");
    });
    await flush();

    fireEvent.click(screen.getByLabelText("Switch project context"));
    await flush();
    fireEvent.click(screen.getByText("后端重构"));
    await flush();

    const ctx = useNewChatContextStore.getState().projectContext;
    expect(ctx).toEqual({
      projectID: 42,
      projectName: "后端重构",
    });
  });

  it("Selecting 无项目 clears the context", async () => {
    appMocks.ListChatAgents.mockResolvedValue({ agents: [] });
    appMocks.ProjectListTree.mockResolvedValue([]);
    renderHarness();

    // 预置一个 context
    await act(async () => {
      useNewChatContextStore.getState().setContext({
        projectID: 42,
        projectName: "后端重构",
      });
      useCommandPaletteStore.getState().openWith("> ");
    });
    await flush();

    fireEvent.click(screen.getByLabelText("Switch project context"));
    await flush();
    fireEvent.click(screen.getByText("No project"));
    await flush();

    expect(useNewChatContextStore.getState().projectContext).toBeNull();
  });
});

describe("CommandPalette — Backspace 键盘流：先清 context，再退出命令模式 (BDD)", () => {
  it("Given empty payload + projectContext set, Then Backspace clears the context but stays in command mode", async () => {
    appMocks.ListChatAgents.mockResolvedValue({ agents: [] });
    appMocks.ProjectListTree.mockResolvedValue([]);
    renderHarness();

    await act(async () => {
      useNewChatContextStore.getState().setContext({
        projectID: 42,
        projectName: "后端重构",
      });
      useCommandPaletteStore.getState().openWith("> ");
    });
    await flush();

    // 还在命令模式
    expect(screen.queryByLabelText("Command mode")).toBeTruthy();

    // 按 Backspace（Input 已聚焦：autoFocus）
    const input = screen.getByRole("combobox");
    fireEvent.keyDown(input, { key: "Backspace" });
    await flush();

    // context 清了
    expect(useNewChatContextStore.getState().projectContext).toBeNull();
    // 但还在命令模式
    expect(screen.queryByLabelText("Command mode")).toBeTruthy();
  });

  it("Given empty payload + NO projectContext, Then Backspace exits command mode (legacy behavior preserved)", async () => {
    appMocks.ListChatAgents.mockResolvedValue({ agents: [] });
    appMocks.ProjectListTree.mockResolvedValue([]);
    renderHarness();
    await act(async () => {
      useCommandPaletteStore.getState().openWith("> ");
    });
    await flush();

    expect(screen.queryByLabelText("Command mode")).toBeTruthy();
    const input = screen.getByRole("combobox");
    fireEvent.keyDown(input, { key: "Backspace" });
    await flush();
    // 退出命令模式（chip 消失）
    expect(screen.queryByLabelText("Command mode")).toBeNull();
  });
});

describe("CommandPalette — last-context persistence (BDD: 默认值 = 上次手动选过的)", () => {
  it("Given user picks a project in palette, When palette is reopened without project-page injecting, Then the last picked project is restored", async () => {
    appMocks.ListChatAgents.mockResolvedValue({ agents: [] });
    appMocks.ProjectListTree.mockResolvedValue([
      {
        project: {
          id: 42,
          parentID: 0,
          name: "后端重构",
          icon: "",
          color: "",
          description: "",
          path: "",
          isGitRepo: false,
          createtime: 0,
          updatetime: 0,
        },
        children: [],
      },
    ]);

    renderHarness();

    // 第一次开：选项目 42
    await act(async () => {
      useCommandPaletteStore.getState().openWith("> ");
    });
    await flush();
    fireEvent.click(screen.getByLabelText("Switch project context"));
    await flush();
    fireEvent.click(screen.getByText("后端重构"));
    await flush();

    // 关 palette + 清 store（模拟切到对话页 / 切到其它 nav，project-page unmount 不再注入）
    await act(async () => {
      useCommandPaletteStore.getState().close();
      useNewChatContextStore.getState().clear();
    });
    await flush();

    // 再打开 palette（⌘N）
    await act(async () => {
      useCommandPaletteStore.getState().openWith("> ");
    });
    await flush();

    // 应回填上次的项目
    const ctx = useNewChatContextStore.getState().projectContext;
    expect(ctx?.projectID).toBe(42);
    expect(ctx?.projectName).toBe("后端重构");
  });

  it("project-page injection wins over localStorage fallback", async () => {
    appMocks.ListChatAgents.mockResolvedValue({ agents: [] });
    appMocks.ProjectListTree.mockResolvedValue([]);
    // 模拟 localStorage 里有"上次"选过的项目 42
    localStorage.setItem(
      "agentre.commandPalette.lastContext",
      JSON.stringify({
        projectID: 42,
        projectName: "旧",
      }),
    );
    renderHarness();
    // 模拟 project-page mount 写入了新项目 99
    await act(async () => {
      useNewChatContextStore.getState().setContext({
        projectID: 99,
        projectName: "新",
      });
      useCommandPaletteStore.getState().openWith("> ");
    });
    await flush();
    // 新的（store 已存）应该胜出
    expect(useNewChatContextStore.getState().projectContext?.projectID).toBe(
      99,
    );
  });

  it("Selecting 无项目 clears localStorage too", async () => {
    appMocks.ListChatAgents.mockResolvedValue({ agents: [] });
    appMocks.ProjectListTree.mockResolvedValue([]);
    localStorage.setItem(
      "agentre.commandPalette.lastContext",
      JSON.stringify({
        projectID: 42,
        projectName: "旧",
      }),
    );

    renderHarness();
    await act(async () => {
      useCommandPaletteStore.getState().openWith("> ");
    });
    await flush();

    fireEvent.click(screen.getByLabelText("Switch project context"));
    await flush();
    // 取最后一个匹配（popover 在 Portal，DOM 后插入；chip 是先存在的）
    const matches = screen.getAllByText("No project");
    fireEvent.click(matches[matches.length - 1]!);
    await flush();

    expect(
      localStorage.getItem("agentre.commandPalette.lastContext"),
    ).toBeNull();
  });
});

describe("CommandPalette — 视觉键盘提示（与 Pencil 设计稿一致）", () => {
  it("ContextBar 右侧显示 Tab 切项目", async () => {
    appMocks.ListChatAgents.mockResolvedValue({ agents: [] });
    appMocks.ProjectListTree.mockResolvedValue([]);
    renderHarness();
    await act(async () => {
      useCommandPaletteStore.getState().openWith("> ");
    });
    await flush();

    expect(screen.getAllByText("Tab").length).toBeGreaterThan(0);
    expect(screen.getByText("Switch project")).toBeTruthy();
  });

  it("Footer 在命令模式下展示 ⌫ 清上下文 提示", async () => {
    appMocks.ListChatAgents.mockResolvedValue({ agents: [] });
    appMocks.ProjectListTree.mockResolvedValue([]);
    renderHarness();
    await act(async () => {
      useCommandPaletteStore.getState().openWith("> ");
    });
    await flush();

    expect(screen.getByText("Clear context")).toBeTruthy();
    expect(screen.getByText("⌫")).toBeTruthy();
  });

  it("Footer 在默认模式下 NOT 显示 ⌫ 清上下文 提示", async () => {
    appMocks.ListChatAgents.mockResolvedValue({ agents: [] });
    appMocks.ProjectListTree.mockResolvedValue([]);
    renderHarness();
    await act(async () => {
      useCommandPaletteStore.getState().toggle(); // 默认模式
    });
    await flush();

    expect(screen.queryByText("Clear context")).toBeNull();
    expect(screen.queryByText("⌫")).toBeNull();
  });
});

describe("CommandPalette — Tab 直接切上下文 (BDD)", () => {
  function mkProject(
    over: Partial<{
      id: number;
      name: string;
    }> = {},
  ) {
    return {
      id: 1,
      parentID: 0,
      name: "项目 A",
      icon: "",
      color: "",
      description: "",
      path: "",
      isGitRepo: false,
      createtime: 0,
      updatetime: 0,
      ...over,
    };
  }

  it("Given projects [A,B] + 无项目, When Tab in Input, Then projectContext === A 且 focus 留在 Input", async () => {
    appMocks.ListChatAgents.mockResolvedValue({ agents: [] });
    appMocks.ProjectListTree.mockResolvedValue([
      { project: mkProject({ id: 10, name: "后端重构" }), children: [] },
      { project: mkProject({ id: 11, name: "前端 UI" }), children: [] },
    ]);
    renderHarness();
    await act(async () => {
      useCommandPaletteStore.getState().openWith("> ");
    });
    await flush();

    const input = screen.getByRole("combobox") as HTMLInputElement;
    input.focus();
    fireEvent.keyDown(input, { key: "Tab" });

    expect(useNewChatContextStore.getState().projectContext?.projectID).toBe(
      10,
    );
    expect(document.activeElement).toBe(input);
  });

  it("Given projectContext === A (idx 0), When Tab, Then projectContext === B (idx 1)", async () => {
    appMocks.ListChatAgents.mockResolvedValue({ agents: [] });
    appMocks.ProjectListTree.mockResolvedValue([
      { project: mkProject({ id: 10, name: "A" }), children: [] },
      { project: mkProject({ id: 11, name: "B" }), children: [] },
    ]);
    renderHarness();
    await act(async () => {
      useNewChatContextStore.getState().setContext({
        projectID: 10,
        projectName: "A",
      });
      useCommandPaletteStore.getState().openWith("> ");
    });
    await flush();

    const input = screen.getByRole("combobox") as HTMLInputElement;
    input.focus();
    fireEvent.keyDown(input, { key: "Tab" });

    expect(useNewChatContextStore.getState().projectContext?.projectID).toBe(
      11,
    );
  });

  it("Given projectContext === B (末尾), When Tab, Then projectContext === null (回到无项目)", async () => {
    appMocks.ListChatAgents.mockResolvedValue({ agents: [] });
    appMocks.ProjectListTree.mockResolvedValue([
      { project: mkProject({ id: 10, name: "A" }), children: [] },
      { project: mkProject({ id: 11, name: "B" }), children: [] },
    ]);
    renderHarness();
    await act(async () => {
      useNewChatContextStore.getState().setContext({
        projectID: 11,
        projectName: "B",
      });
      useCommandPaletteStore.getState().openWith("> ");
    });
    await flush();

    const input = screen.getByRole("combobox") as HTMLInputElement;
    input.focus();
    fireEvent.keyDown(input, { key: "Tab" });

    expect(useNewChatContextStore.getState().projectContext).toBeNull();
  });

  it("Given projectContext === B (idx 1), When Shift+Tab, Then projectContext === A (idx 0)", async () => {
    appMocks.ListChatAgents.mockResolvedValue({ agents: [] });
    appMocks.ProjectListTree.mockResolvedValue([
      { project: mkProject({ id: 10, name: "A" }), children: [] },
      { project: mkProject({ id: 11, name: "B" }), children: [] },
    ]);
    renderHarness();
    await act(async () => {
      useNewChatContextStore.getState().setContext({
        projectID: 11,
        projectName: "B",
      });
      useCommandPaletteStore.getState().openWith("> ");
    });
    await flush();

    const input = screen.getByRole("combobox") as HTMLInputElement;
    input.focus();
    fireEvent.keyDown(input, { key: "Tab", shiftKey: true });

    expect(useNewChatContextStore.getState().projectContext).toEqual({
      projectID: 10,
      projectName: "A",
    });
    expect(document.activeElement).toBe(input);
  });

  it("Given projectContext === null, When Shift+Tab, Then projectContext === last project", async () => {
    appMocks.ListChatAgents.mockResolvedValue({ agents: [] });
    appMocks.ProjectListTree.mockResolvedValue([
      { project: mkProject({ id: 10, name: "A" }), children: [] },
      { project: mkProject({ id: 11, name: "B" }), children: [] },
    ]);
    renderHarness();
    await act(async () => {
      useCommandPaletteStore.getState().openWith("> ");
    });
    await flush();

    const input = screen.getByRole("combobox") as HTMLInputElement;
    input.focus();
    fireEvent.keyDown(input, { key: "Tab", shiftKey: true });

    expect(useNewChatContextStore.getState().projectContext).toEqual({
      projectID: 11,
      projectName: "B",
    });
  });

  it("Given 无项目列表 + 无 context, When Tab, Then projectContext 保持 null (no-op)", async () => {
    appMocks.ListChatAgents.mockResolvedValue({ agents: [] });
    appMocks.ProjectListTree.mockResolvedValue([]);
    renderHarness();
    await act(async () => {
      useCommandPaletteStore.getState().openWith("> ");
    });
    await flush();

    const input = screen.getByRole("combobox") as HTMLInputElement;
    input.focus();
    fireEvent.keyDown(input, { key: "Tab" });

    expect(useNewChatContextStore.getState().projectContext).toBeNull();
  });

  it("默认模式 (无 > 前缀): Tab 在 Input 中不改 projectContext", async () => {
    appMocks.ListChatAgents.mockResolvedValue({ agents: [] });
    appMocks.ProjectListTree.mockResolvedValue([
      { project: mkProject({ id: 10, name: "A" }), children: [] },
    ]);
    renderHarness();
    await act(async () => {
      useCommandPaletteStore.getState().toggle(); // 默认模式打开
    });
    await flush();

    expect(screen.queryByLabelText("Switch project context")).toBeNull();

    const input = screen.getByRole("combobox") as HTMLInputElement;
    input.focus();
    fireEvent.keyDown(input, { key: "Tab" });

    expect(useNewChatContextStore.getState().projectContext).toBeNull();
  });
});

describe("CommandPalette — 路由互斥的两个命令 source", () => {
  it("/chat 路由 ⌘N → 只显示 'New chat with X'，不显示 'New project chat with' + 无 ContextBar", async () => {
    appMocks.ListChatAgents.mockResolvedValue({
      agents: [mkAgent({ id: 1, name: "CEO 助手" })],
    });
    appMocks.ProjectListTree.mockResolvedValue([]);
    renderHarness("/chat");
    await act(async () => {
      useCommandPaletteStore.getState().openWith("> ");
    });
    await flush();

    expect(screen.getByText("New chat with")).toBeTruthy();
    expect(screen.queryByText("New project chat with")).toBeNull();
    // ContextBar 不渲染（自由 source 不需要项目作用域）
    expect(screen.queryByLabelText("Switch project context")).toBeNull();
  });

  it("/projects 路由 ⌘N → 只显示 'New project chat with X'，不显示自由 'New chat with' + ContextBar 可见", async () => {
    appMocks.ListChatAgents.mockResolvedValue({
      agents: [mkAgent({ id: 1, name: "CEO 助手" })],
    });
    appMocks.ProjectListTree.mockResolvedValue([]);
    renderHarness("/projects");
    await act(async () => {
      useCommandPaletteStore.getState().openWith("> ");
    });
    await flush();

    expect(screen.getByText("New project chat with")).toBeTruthy();
    expect(screen.queryByText("New chat with")).toBeNull();
    // ContextBar 可见
    expect(screen.getByLabelText("Switch project context")).toBeTruthy();
  });

  it("/chat 路由 ⌘N + Tab → projectContext 不变（自由命令不消费 Tab）", async () => {
    appMocks.ListChatAgents.mockResolvedValue({ agents: [] });
    appMocks.ProjectListTree.mockResolvedValue([
      {
        project: {
          id: 10,
          parentID: 0,
          name: "A",
          icon: "",
          color: "",
          description: "",
          path: "",
          isGitRepo: false,
          createtime: 0,
          updatetime: 0,
        },
        children: [],
      },
    ]);
    renderHarness("/chat");
    await act(async () => {
      useCommandPaletteStore.getState().openWith("> ");
    });
    await flush();

    const input = screen.getByRole("combobox") as HTMLInputElement;
    input.focus();
    fireEvent.keyDown(input, { key: "Tab" });

    // /chat 路由：Tab handler 不拦截 → projectContext 没被赋值
    expect(useNewChatContextStore.getState().projectContext).toBeNull();
  });

  it("/projects 路由 ⌘N + 输入 'new project chat ce' → 只命中含 'CEO' 的 New project chat with 行", async () => {
    appMocks.ListChatAgents.mockResolvedValue({
      agents: [
        mkAgent({ id: 1, name: "CEO 助手" }),
        mkAgent({ id: 2, name: "工程师" }),
      ],
    });
    appMocks.ProjectListTree.mockResolvedValue([]);
    renderHarness("/projects");
    await act(async () => {
      useCommandPaletteStore.getState().openWith("> new project chat ce");
    });
    await flush();

    expect(screen.getByText("CEO 助手")).toBeTruthy();
    expect(screen.queryByText("工程师")).toBeNull();
  });

  it("/projects 路由 ⌘N reopen → refetches project members instead of reusing a stale empty set", async () => {
    appMocks.ListChatAgents.mockResolvedValue({
      agents: [
        mkAgent({ id: 5, name: "Builder" }),
        mkAgent({ id: 6, name: "Reviewer" }),
      ],
    });
    appMocks.ProjectGet.mockResolvedValueOnce({
      project: { id: 1, name: "Agentre" },
      directMembers: [],
      inheritedMembers: [],
    }).mockResolvedValueOnce({
      project: { id: 1, name: "Agentre" },
      directMembers: [{ agentID: 5 }],
      inheritedMembers: [],
    });
    useNewChatContextStore
      .getState()
      .setContext({ projectID: 1, projectName: "Agentre" });

    renderHarness("/projects");
    await act(async () => {
      useCommandPaletteStore.getState().openWith("> ");
    });
    await flush();
    expect(appMocks.ProjectGet).toHaveBeenCalledTimes(1);
    expect(screen.getByText("Other Agents")).toBeTruthy();

    await act(async () => {
      useCommandPaletteStore.getState().close();
    });
    await flush();
    await act(async () => {
      useCommandPaletteStore.getState().openWith("> ");
    });
    await flush();

    expect(appMocks.ProjectGet).toHaveBeenCalledTimes(2);
    expect(await screen.findByText("New chat in Agentre")).toBeTruthy();
  });
});

describe("CommandPalette — 非成员（其它 Agent）行不可选/不可点（disabled）", () => {
  it("/projects: 非成员行 aria-disabled=true + cursor-not-allowed；成员行可选", async () => {
    appMocks.ListChatAgents.mockResolvedValue({
      agents: [
        mkAgent({ id: 5, name: "Builder" }), // member
        mkAgent({ id: 6, name: "Outsider" }), // non-member（其它 Agent）
      ],
    });
    appMocks.ProjectGet.mockResolvedValue({
      project: { id: 1, name: "Agentre" },
      directMembers: [{ agentID: 5 }],
      inheritedMembers: [],
    });
    useNewChatContextStore
      .getState()
      .setContext({ projectID: 1, projectName: "Agentre" });

    renderHarness("/projects");
    await act(async () => {
      useCommandPaletteStore.getState().openWith("> ");
    });
    await flush();

    // 非成员行：cmdk-item 被标记 disabled（不可键盘导航 / 不可点）
    const outsiderItem = screen.getByText("Outsider").closest("[cmdk-item]");
    expect(outsiderItem?.getAttribute("aria-disabled")).toBe("true");
    expect(outsiderItem?.className).toContain("cursor-not-allowed");
    expect(outsiderItem?.className).not.toContain("cursor-pointer");

    // 成员行：未被禁用
    const builderItem = screen.getByText("Builder").closest("[cmdk-item]");
    expect(builderItem?.getAttribute("aria-disabled")).toBe("false");
    expect(builderItem?.className).toContain("cursor-pointer");
  });
});

describe("CommandPalette — ⌘P does NOT enter command mode after a prior ⌘N (regression for stale initialQuery)", () => {
  it("Given ⌘N → close (setOpen false) → ⌘P (toggle), Then palette is in default mode", async () => {
    appMocks.ListChatAgents.mockResolvedValue({
      agents: [mkAgent({ id: 1, name: "CEO 助手" })],
    });
    renderHarness();

    await act(async () => {
      useCommandPaletteStore.getState().openWith("> ");
    });
    await flush();
    expect(screen.queryByLabelText("Command mode")).toBeTruthy();

    await act(async () => {
      useCommandPaletteStore.getState().setOpen(false);
    });
    await flush();

    await act(async () => {
      useCommandPaletteStore.getState().toggle();
    });
    await flush();

    expect(screen.queryByLabelText("Command mode")).toBeNull();
    expect(screen.queryByText("New chat with")).toBeNull();
  });

  it("Given ⌘N → close → ⌘N again, Then second open still has the seed (single-fire consumption is per-open)", async () => {
    appMocks.ListChatAgents.mockResolvedValue({
      agents: [mkAgent({ id: 1, name: "CEO 助手" })],
    });
    renderHarness();

    await act(async () => {
      useCommandPaletteStore.getState().openWith("> ");
    });
    await flush();
    expect(screen.queryByLabelText("Command mode")).toBeTruthy();

    await act(async () => {
      useCommandPaletteStore.getState().close();
    });
    await flush();

    await act(async () => {
      useCommandPaletteStore.getState().openWith("> ");
    });
    await flush();
    expect(screen.queryByLabelText("Command mode")).toBeTruthy();
  });
});
