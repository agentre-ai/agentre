import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ChatPanelHost } from "../chat-panel-host";
import { useChatAgentsStore } from "@/stores/chat-agents-store";
import { useChatTabsStore } from "@/stores/chat-tabs-store";
import { useProjectSessionsStore } from "@/stores/project-sessions-store";

// 把 onSidebarShouldReload 通过 data-attribute 暴露到 DOM 上, 这样回归测试可以
// 拿到这个回调并断言它真的去触发 store.reload (修复「新建会话不进左栏」的关键路径)。
type ChatPanelStub = {
  sessionId: number;
  newSessionAgent?: { id: number; name: string } | null;
  onSidebarShouldReload?: () => void;
};
vi.mock("../../chat-panel", () => ({
  ChatPanel: ({
    sessionId,
    newSessionAgent,
    onSidebarShouldReload,
  }: ChatPanelStub) => (
    <div
      data-testid={`chat-panel-${sessionId}`}
      data-agent-id={newSessionAgent?.id ?? ""}
    >
      <button
        type="button"
        data-testid={`fire-reload-${sessionId}`}
        onClick={() => onSidebarShouldReload?.()}
      >
        fire
      </button>
      {newSessionAgent ? <span>new agent {newSessionAgent.name}</span> : null}
      session {sessionId}
    </div>
  ),
}));

describe("ChatPanelHost", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useChatTabsStore.setState({ tabs: [], activeTabId: null });
    useChatAgentsStore.getState().__reset();
    useProjectSessionsStore.getState().__reset();
  });

  it("空 tab 显示统一空态 hero", () => {
    render(<ChatPanelHost />);
    expect(
      screen.getByText(/选一个 Agent 或项目下的会话开始/),
    ).toBeInTheDocument();
  });

  it("每个 kind:'session' tab 渲染一个 ChatPanel mount", () => {
    useChatTabsStore.getState().openSessionInNewTab(1);
    useChatTabsStore.getState().openSessionInNewTab(2);
    render(<ChatPanelHost />);
    expect(screen.getByTestId("chat-panel-1")).toBeInTheDocument();
    expect(screen.getByTestId("chat-panel-2")).toBeInTheDocument();
  });

  it("非 active tab 的 ChatPanel 容器 display:none", () => {
    useChatTabsStore.getState().openSessionInNewTab(1);
    useChatTabsStore.getState().openSessionInNewTab(2);
    const firstId = useChatTabsStore.getState().tabs[0].id;
    useChatTabsStore.getState().setActive(firstId);
    render(<ChatPanelHost />);
    const wrap2 = screen.getByTestId("chat-panel-2").parentElement!;
    expect(wrap2).toHaveStyle({ display: "none" });
  });

  it("kind:'new' tab 从 chat-agents-store 取 agent 并渲染新会话面板", () => {
    useChatAgentsStore.setState({
      agents: [
        {
          id: 7,
          name: "Eng",
          avatarColor: "agent-1",
          backendType: "builtin",
          chattable: true,
          pinned: false,
          sessions: [],
          attentionSessions: [],
          sessionIds: [],
        },
      ] as never,
      loading: false,
      error: null,
    });
    useChatTabsStore.getState().openNewSession(0, 7, "");

    render(<ChatPanelHost />);

    const panel = screen.getByTestId("chat-panel-0");
    expect(panel).toHaveAttribute("data-agent-id", "7");
    expect(screen.getByText("new agent Eng")).toBeInTheDocument();
  });

  it("kind:'new' tab 暂时找不到 agent 时刷新 chat-agents-store 且不渲染空白", () => {
    const reload = vi
      .spyOn(useChatAgentsStore.getState(), "reload")
      .mockResolvedValue();
    useChatAgentsStore.setState({ agents: [], loading: true, error: null });
    useChatTabsStore.getState().openNewSession(0, 99, "");

    render(<ChatPanelHost />);

    expect(screen.getByText("正在加载 Agent 信息…")).toBeInTheDocument();
    expect(screen.getByText("Agent #99")).toBeInTheDocument();
    expect(screen.queryByTestId("chat-panel-0")).not.toBeInTheDocument();
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it("ChatPanel 触发 onSidebarShouldReload 同步刷新 chat-agents + project-sessions (修复新建会话不进左栏)", () => {
    const chatReload = vi
      .spyOn(useChatAgentsStore.getState(), "reload")
      .mockResolvedValue();
    const projectReload = vi
      .spyOn(useProjectSessionsStore.getState(), "reload")
      .mockResolvedValue();
    useChatTabsStore.getState().openSessionInNewTab(42);
    render(<ChatPanelHost />);
    const chatCallsBeforeClick = chatReload.mock.calls.length;
    const projectCallsBeforeClick = projectReload.mock.calls.length;
    screen.getByTestId("fire-reload-42").click();
    // 两个 sidebar 数据源都该被一次性触发 —— 这样 /chat 和 /projects 的左栏
    // 在新建会话 / turn 落定后都能立刻看到变化, 不必等下一次 mount。
    expect(chatReload).toHaveBeenCalledTimes(chatCallsBeforeClick + 1);
    expect(projectReload).toHaveBeenCalledTimes(projectCallsBeforeClick + 1);
    chatReload.mockRestore();
    projectReload.mockRestore();
  });
});
