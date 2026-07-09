import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

// conversation-panel.tsx 不再直接 import wailsjs,但它 transitively 引入
// permission-mode barrel(use-permission-mode.ts 里静态 import 了
// wailsjs/go/app/App 的 SetChatPermissionMode)。per-file mock 保持隔离、不碰真
// wails runtime。
vi.mock("../../../../../wailsjs/runtime/runtime", () => ({
  EventsOn: () => () => {},
  EventsOff: () => {},
  EventsEmit: () => {},
}));
vi.mock("../../../../../wailsjs/go/app/App", () => ({
  RunLoad: vi.fn(),
  SetChatPermissionMode: vi.fn(),
}));

const liveConv = {
  messages: [{ id: 1, role: "assistant" }],
  live: {
    liveDelta: "streaming-text",
    liveThinking: "",
    liveBlocks: [],
    liveRetry: null,
    liveStreamStartedAt: 1,
    liveTargetId: 200,
    streaming: true,
    liveCompacting: false,
  },
  submit: vi.fn(),
  sending: false,
  isModeSwitchable: false,
  supportsImageInput: true,
  permissionMode: { mode: "default" },
  permissionModeMeta: { order: [] },
  backendType: "claudecode",
  contextUsage: { used: 0, max: 0 },
};
vi.mock("@/hooks/use-live-conversation", () => ({
  useLiveConversation: () => liveConv,
}));

// ChatTranscript / ChatComposer 是重组件，这里 stub 成可断言 props 透传的轻量占位
vi.mock("../../chat", () => ({
  ChatTranscript: (p: { liveDelta?: string; liveTargetId?: number | null }) => (
    <div
      data-testid="tx"
      data-live={p.liveDelta}
      data-target={p.liveTargetId ?? ""}
    />
  ),
  ChatComposer: (p: { onSubmit?: (m: unknown) => void }) => (
    <button
      type="button"
      data-testid="composer"
      onClick={() => p.onSubmit?.({ text: "hello" })}
    >
      send
    </button>
  ),
}));

import { useOrchRunStore } from "../../../../stores/orch-run-store";
import { ConversationPanel } from "../conversation-panel";

beforeEach(() => {
  useOrchRunStore.getState().__reset();
  liveConv.submit.mockClear();
});

describe("ConversationPanel", () => {
  it("返回按钮调 onBack", () => {
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

  it("渲染 cvHead: 返回按钮 + agentName 展示", () => {
    render(
      <ConversationPanel
        sessionId={701}
        agentName="后端工程师"
        agentColor="agent-2"
        onBack={vi.fn()}
      />,
    );
    expect(screen.getByTestId("conversation-back")).toBeInTheDocument();
    expect(screen.getByTestId("conversation-who-name")).toHaveTextContent(
      "后端工程师",
    );
  });

  it("只读 transcript: 无 Edit/Regenerate 按钮(no onRerun/onEdit props)", () => {
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

  it("等待高亮: runId+agentId 提供且 activeAsks 含该 agent → 展示 waiting callout", () => {
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
    expect(screen.queryByTestId("conversation-awaiting-callout")).toBeNull();
  });

  it("who-name 展示 agentName + session suffix", () => {
    render(
      <ConversationPanel
        sessionId={701}
        agentName="后端工程师"
        agentColor="agent-2"
        onBack={vi.fn()}
      />,
    );
    expect(screen.getByTestId("conversation-who-name")).toHaveTextContent(
      "后端工程师",
    );
    // suffix label is rendered (i18n key: sessionLabel → "session" in en locale)
    expect(screen.getByTestId("conversation-who-name")).toHaveTextContent(
      "session",
    );
  });

  it("who-subtitle: 无 runId 时渲染 idle 状态标签 + 0 任务", () => {
    render(
      <ConversationPanel
        sessionId={701}
        agentName="后端"
        agentColor="agent-2"
        onBack={vi.fn()}
      />,
    );
    expect(screen.getByTestId("conversation-who-subtitle")).toBeInTheDocument();
  });

  it("who-subtitle: runId+agentId 有 running 任务 → 展示 running 状态 + 任务数", () => {
    useOrchRunStore.setState({
      details: new Map([
        [
          10,
          {
            run: { id: 10, status: "running" } as never,
            dispatches: [
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
    expect(subtitle).toBeInTheDocument();
    expect(subtitle).toHaveTextContent("2");
  });

  it("who-subtitle: 0 任务(pending) → 不显示 Done，显示 Pending", () => {
    useOrchRunStore.setState({
      details: new Map([
        [
          30,
          {
            run: { id: 30, status: "running" } as never,
            dispatches: [{ id: 1, agentId: 99, status: "running" } as never],
          } as never,
        ],
      ]),
    });

    render(
      <ConversationPanel
        sessionId={801}
        agentName="新 Agent"
        agentColor="agent-4"
        onBack={vi.fn()}
        runId={30}
        agentId={8}
      />,
    );
    const subtitle = screen.getByTestId("conversation-who-subtitle");
    expect(subtitle).not.toHaveTextContent("Done");
    expect(subtitle).toHaveTextContent("Pending");
    expect(subtitle).toHaveTextContent("0");
  });

  it("who-subtitle: 全 done 任务 → done 状态 + 任务数", () => {
    useOrchRunStore.setState({
      details: new Map([
        [
          20,
          {
            run: { id: 20, status: "running" } as never,
            dispatches: [
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
    expect(subtitle).toHaveTextContent("2");
  });

  it("who-subtitle: 有任务但未全完成且无 running(idle)→ 不显示 Done，显示 In progress", () => {
    useOrchRunStore.setState({
      details: new Map([
        [
          40,
          {
            run: { id: 40, status: "running" } as never,
            dispatches: [
              { id: 1, agentId: 5, status: "done" } as never,
              { id: 2, agentId: 5, status: "pending" } as never,
            ],
          } as never,
        ],
      ]),
    });

    render(
      <ConversationPanel
        sessionId={801}
        agentName="后端"
        agentColor="agent-2"
        onBack={vi.fn()}
        runId={40}
        agentId={5}
      />,
    );
    const subtitle = screen.getByTestId("conversation-who-subtitle");
    expect(subtitle).not.toHaveTextContent("Done");
    expect(subtitle).toHaveTextContent("In progress");
    expect(subtitle).toHaveTextContent("2");
  });

  it("who-row 渲染状态点 testid", () => {
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

  // ── Task 3: 流式化(live overlay 透传 ChatTranscript + 换 ChatComposer) ────

  it("把 live overlay 透传给 ChatTranscript", () => {
    render(
      <ConversationPanel
        sessionId={7}
        agentName="Alice"
        agentColor="agent-1"
        onBack={() => {}}
        runId={1}
        agentId={3}
      />,
    );
    expect(screen.getByTestId("tx").getAttribute("data-live")).toBe(
      "streaming-text",
    );
    // liveTargetId 必须一并透传:否则 ChatTranscript 找不到「哪条消息挂 live tail」,
    // 流式文字不会渲染 —— 编排会话就不会像对话模块那样流式输出。
    expect(screen.getByTestId("tx").getAttribute("data-target")).toBe("200");
  });

  it("ChatComposer onSubmit → hook.submit", () => {
    render(
      <ConversationPanel
        sessionId={7}
        agentName="Alice"
        agentColor="agent-1"
        onBack={() => {}}
        runId={1}
        agentId={3}
      />,
    );
    screen.getByTestId("composer").click();
    expect(liveConv.submit).toHaveBeenCalledWith(
      expect.objectContaining({ text: "hello" }),
    );
  });
});
