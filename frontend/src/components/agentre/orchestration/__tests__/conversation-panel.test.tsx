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

import { useOrchSubagentsStore } from "../../../../stores/orch-subagents-store";
import { ConversationPanel } from "../conversation-panel";

beforeEach(() => {
  useOrchSubagentsStore.getState().__reset();
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
});
