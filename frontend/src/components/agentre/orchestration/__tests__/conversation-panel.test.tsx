import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const runSpeak = vi.fn().mockResolvedValue(undefined);
const loadSession = vi.fn();
vi.mock("../../../../../wailsjs/go/app/App", () => ({
  RunSpeak: (...a: unknown[]) => runSpeak(...a),
  LoadChatSession: (...a: unknown[]) => loadSession(...a),
  ListChatAgents: vi.fn().mockResolvedValue({ agents: [] }),
}));
// ChatTranscript 是重组件,这里 stub 成可断言消息数的轻量占位
vi.mock("../../chat", () => ({
  ChatTranscript: ({ messages }: { messages: unknown[] }) => (
    <div data-testid="stub-transcript">{messages.length}</div>
  ),
}));

import { useOrchRunStore } from "../../../../stores/orch-run-store";
import { ConversationPanel } from "../conversation-panel";

beforeEach(() => {
  useOrchRunStore.getState().__reset();
  runSpeak.mockClear();
  loadSession.mockReset();
});

describe("ConversationPanel", () => {
  it("加载该 session 并把 messages 喂给 ChatTranscript", async () => {
    loadSession.mockResolvedValue({
      messages: [
        { id: 1, blocks: [] },
        { id: 2, blocks: [] },
      ],
    });
    render(
      <ConversationPanel
        sessionId={701}
        agentName="后端"
        agentColor="agent-2"
        onBack={vi.fn()}
      />,
    );
    await waitFor(() =>
      expect(screen.getByTestId("stub-transcript")).toHaveTextContent("2"),
    );
  });

  it("返回按钮调 onBack", () => {
    loadSession.mockResolvedValue({ messages: [] });
    const onBack = vi.fn();
    render(
      <ConversationPanel
        sessionId={701}
        agentName="后端"
        agentColor="agent-2"
        onBack={onBack}
      />,
    );
    fireEvent.click(screen.getByTestId("conversation-back"));
    expect(onBack).toHaveBeenCalled();
  });

  it("对它说 → RunSpeak(sessionId, text) 并清空输入", async () => {
    loadSession.mockResolvedValue({ messages: [] });
    render(
      <ConversationPanel
        sessionId={701}
        agentName="后端"
        agentColor="agent-2"
        onBack={vi.fn()}
      />,
    );
    const input = screen.getByTestId(
      "conversation-speak-input",
    ) as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: "改用 sqlmock" } });
    fireEvent.click(screen.getByTestId("conversation-speak-send"));
    await waitFor(() =>
      expect(runSpeak).toHaveBeenCalledWith(701, "改用 sqlmock"),
    );
    await waitFor(() => expect(input.value).toBe(""));
  });

  it("RunSpeak 成功后调 LoadChatSession reload，刷新显示已发消息", async () => {
    // 初始加载返回空
    loadSession.mockResolvedValueOnce({ messages: [] });
    render(
      <ConversationPanel
        sessionId={702}
        agentName="后端"
        agentColor="agent-2"
        onBack={vi.fn()}
      />,
    );
    // 等初始加载完成
    await waitFor(() => expect(loadSession).toHaveBeenCalledTimes(1));

    // RunSpeak 成功后 reload 应再次调 LoadChatSession
    loadSession.mockResolvedValueOnce({
      messages: [{ id: 1, blocks: [{ type: "text", text: "改用 sqlmock" }] }],
    });
    const input = screen.getByTestId(
      "conversation-speak-input",
    ) as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: "改用 sqlmock" } });
    fireEvent.click(screen.getByTestId("conversation-speak-send"));

    await waitFor(() =>
      expect(runSpeak).toHaveBeenCalledWith(702, "改用 sqlmock"),
    );
    // reload 调 LoadChatSession 第 2 次
    await waitFor(() => expect(loadSession).toHaveBeenCalledTimes(2));
    // transcript 展示 reload 后的消息数
    await waitFor(() =>
      expect(screen.getByTestId("stub-transcript")).toHaveTextContent("1"),
    );
  });

  // ── 新 cvHead/cvInput 结构断言 (Step 1 RED) ────────────────────────────

  it("渲染 cvHead: 返回按钮 + agentName 展示", () => {
    loadSession.mockResolvedValue({ messages: [] });
    render(
      <ConversationPanel
        sessionId={701}
        agentName="后端工程师"
        agentColor="agent-2"
        onBack={vi.fn()}
      />,
    );
    // 返回按钮存在
    expect(screen.getByTestId("conversation-back")).toBeInTheDocument();
    // agent name 展示在 who 行
    expect(screen.getByTestId("conversation-who-name")).toHaveTextContent(
      "后端工程师",
    );
  });

  it("cvInput: 输入框与发送按钮均渲染", () => {
    loadSession.mockResolvedValue({ messages: [] });
    render(
      <ConversationPanel
        sessionId={701}
        agentName="后端"
        agentColor="agent-2"
        onBack={vi.fn()}
      />,
    );
    expect(screen.getByTestId("conversation-speak-input")).toBeInTheDocument();
    expect(screen.getByTestId("conversation-speak-send")).toBeInTheDocument();
  });

  it("只读 transcript: 无 Edit/Regenerate 按钮(no onRerun/onEdit props)", () => {
    loadSession.mockResolvedValue({ messages: [] });
    render(
      <ConversationPanel
        sessionId={701}
        agentName="后端"
        agentColor="agent-2"
        onBack={vi.fn()}
      />,
    );
    // stub 不渲染 edit/rerun 按钮
    expect(screen.queryByTestId("message-rerun")).toBeNull();
    expect(screen.queryByTestId("message-edit")).toBeNull();
  });

  it("等待高亮: runId+agentId 提供且 activeAsks 含该 agent → 展示 waiting callout", async () => {
    loadSession.mockResolvedValue({ messages: [] });
    // 直接注入 activeAsks 状态(跳过 onRunEvent 以避免触发 RunLoad)
    const asks = new Map([
      [10, [{ askId: "ask-1", askerAgentId: 5, targetAgentId: 99 }]],
    ]);
    useOrchRunStore.setState({ activeAsks: asks });

    render(
      <ConversationPanel
        sessionId={701}
        agentName="后端"
        agentColor="agent-2"
        onBack={vi.fn()}
        runId={10}
        agentId={5}
      />,
    );
    expect(
      screen.getByTestId("conversation-awaiting-callout"),
    ).toBeInTheDocument();
  });

  it("等待高亮: agentId 不在 activeAsks 中时不展示 waiting callout", () => {
    loadSession.mockResolvedValue({ messages: [] });
    render(
      <ConversationPanel
        sessionId={701}
        agentName="后端"
        agentColor="agent-2"
        onBack={vi.fn()}
        runId={10}
        agentId={999}
      />,
    );
    // no active ask for agentId=999
    expect(screen.queryByTestId("conversation-awaiting-callout")).toBeNull();
  });

  // ── Finding #3: who-row 副标题 + 状态点 + "· 会话" 后缀 (RED→GREEN) ─────

  it("who-name 展示 agentName + session suffix", () => {
    loadSession.mockResolvedValue({ messages: [] });
    render(
      <ConversationPanel
        sessionId={701}
        agentName="后端工程师"
        agentColor="agent-2"
        onBack={vi.fn()}
      />,
    );
    // name line includes agentName
    expect(screen.getByTestId("conversation-who-name")).toHaveTextContent(
      "后端工程师",
    );
    // suffix label is rendered (i18n key: sessionLabel → "session" in en locale)
    expect(screen.getByTestId("conversation-who-name")).toHaveTextContent(
      "session",
    );
  });

  it("who-subtitle: 无 runId 时渲染 idle 状态标签 + 0 任务", () => {
    loadSession.mockResolvedValue({ messages: [] });
    render(
      <ConversationPanel
        sessionId={701}
        agentName="后端"
        agentColor="agent-2"
        onBack={vi.fn()}
      />,
    );
    // subtitle element is present
    expect(screen.getByTestId("conversation-who-subtitle")).toBeInTheDocument();
  });

  it("who-subtitle: runId+agentId 有 running 任务 → 展示 running 状态 + 任务数", () => {
    loadSession.mockResolvedValue({ messages: [] });
    // inject detail with tasks for agentId=5 in run 10
    useOrchRunStore.setState({
      details: new Map([
        [
          10,
          {
            run: { id: 10, status: "running" } as never,
            tasks: [
              { id: 1, agentId: 5, status: "running" } as never,
              { id: 2, agentId: 5, status: "done" } as never,
              { id: 3, agentId: 99, status: "running" } as never, // different agent
            ],
          } as never,
        ],
      ]),
    });

    render(
      <ConversationPanel
        sessionId={701}
        agentName="后端"
        agentColor="agent-2"
        onBack={vi.fn()}
        runId={10}
        agentId={5}
      />,
    );
    const subtitle = screen.getByTestId("conversation-who-subtitle");
    // agentId=5 has 2 tasks; one is running → status label "Running" / "运行中"
    expect(subtitle).toBeInTheDocument();
    // task count: 2 tasks for agentId=5
    expect(subtitle).toHaveTextContent("2");
  });

  it("who-subtitle: 全 done 任务 → done 状态 + 任务数", () => {
    loadSession.mockResolvedValue({ messages: [] });
    useOrchRunStore.setState({
      details: new Map([
        [
          20,
          {
            run: { id: 20, status: "running" } as never,
            tasks: [
              { id: 1, agentId: 7, status: "done" } as never,
              { id: 2, agentId: 7, status: "done" } as never,
            ],
          } as never,
        ],
      ]),
    });

    render(
      <ConversationPanel
        sessionId={801}
        agentName="前端"
        agentColor="agent-3"
        onBack={vi.fn()}
        runId={20}
        agentId={7}
      />,
    );
    const subtitle = screen.getByTestId("conversation-who-subtitle");
    // all done → done status, count=2
    expect(subtitle).toHaveTextContent("2");
  });

  it("who-row 渲染状态点 testid", () => {
    loadSession.mockResolvedValue({ messages: [] });
    render(
      <ConversationPanel
        sessionId={701}
        agentName="后端"
        agentColor="agent-2"
        onBack={vi.fn()}
      />,
    );
    expect(
      screen.getByTestId("conversation-who-status-dot"),
    ).toBeInTheDocument();
  });
});
