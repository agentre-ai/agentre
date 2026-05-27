import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ChatTranscript } from "@/components/agentre/chat";
import type { ChatBlockData } from "@/stores/chat-streams-store";
import type { chat_svc } from "../../../../wailsjs/go/models";

function renderTranscriptWithSubagent() {
  render(
    <ChatTranscript
      agentColor="agent-1"
      agentName="CEO 助手"
      messages={[
        chatMessage([
          {
            toolInput: {
              description: "probe",
              prompt: "Run echo hello and return.",
              subagent_type: "general-purpose",
            },
            toolName: "Agent",
            toolUseId: "toolu-parent",
            type: "tool_use",
            canonical: {
              kind: "agent.spawn",
              agentSpawn: {
                taskId: "toolu-parent",
                subagentType: "general-purpose",
                taskDescription: "probe",
                prompt: "Run echo hello and return.",
                toolUses: 1,
                totalTokens: 14500,
                durationMs: 7800,
                lastToolName: "Bash",
                status: "completed",
              },
            },
          } as unknown as ChatBlockData,
          {
            parentToolUseId: "toolu-parent",
            toolInput: { command: "echo hello" },
            toolName: "Bash",
            toolUseId: "toolu-child",
            type: "tool_use",
          } as ChatBlockData,
          {
            parentToolUseId: "toolu-parent",
            text: "hello",
            toolUseId: "toolu-child",
            type: "tool_result",
          } as ChatBlockData,
          {
            text: "Raw output:\n```\nhello\n```",
            toolUseId: "toolu-parent",
            type: "tool_result",
          } as ChatBlockData,
        ]),
      ]}
    />,
  );
  return screen.getByRole("region", { name: /^Subagent/ });
}

function chatMessage(blocks: ChatBlockData[]): chat_svc.ChatMessage {
  return {
    blocks,
    completionTokens: 0,
    createtime: new Date("2026-05-17T10:30:00Z").getTime(),
    durationMs: 0,
    errorText: "",
    id: 1,
    model: "",
    promptTokens: 0,
    role: "assistant",
    seq: 1,
    sessionId: 1,
  } as chat_svc.ChatMessage;
}

function mockTextSelectionWithin(node: Node) {
  const range = { commonAncestorContainer: node } as Range;
  return vi.spyOn(window, "getSelection").mockReturnValue({
    anchorNode: node,
    focusNode: node,
    getRangeAt: () => range,
    isCollapsed: false,
    rangeCount: 1,
    toString: () => "selected",
  } as unknown as Selection);
}

describe("ChatTranscript message meta", () => {
  function assistantWithUsage(): chat_svc.ChatMessage {
    return {
      blocks: [{ type: "text", text: "hi" } as ChatBlockData],
      cacheCreationTokens: 11,
      cachedTokens: 22,
      completionTokens: 50,
      createtime: new Date("2026-05-17T10:30:00Z").getTime(),
      durationMs: 1200,
      errorText: "",
      id: 7,
      model: "claude-sonnet-4-6",
      promptTokens: 100,
      reasoningTokens: 33,
      role: "assistant",
      seq: 1,
      sessionId: 1,
    } as chat_svc.ChatMessage;
  }

  it("renders prompt/completion as inline arrow counters and exposes a tooltip trigger", () => {
    render(
      <ChatTranscript
        agentColor="agent-1"
        agentName="CEO 助手"
        messages={[assistantWithUsage()]}
        onRerun={() => undefined}
      />,
    );

    const trigger = screen.getByRole("button", { name: "token 用量明细" });
    expect(trigger).toHaveTextContent("claude-sonnet-4-6");
    expect(trigger).toHaveTextContent("100");
    expect(trigger).toHaveTextContent("50");
    expect(trigger).toHaveTextContent("1.2s");
    expect(within(trigger).queryByText("tokens")).not.toBeInTheDocument();
  });

  it("renders the meta strip below the message, always visible without hover gating", () => {
    // 之前用 group-hover / React state 控制 meta 显隐，在 Wails WebKit 下
    // 多次出现 meta 一直亮起的 bug。现在改成常驻显示，靠 text-subtle-foreground
    // + text-2xs 自身弱化样式，不再依赖任何 hover/focus 状态。
    render(
      <ChatTranscript
        agentColor="agent-1"
        agentName="CEO 助手"
        messages={[assistantWithUsage()]}
        onRerun={() => undefined}
      />,
    );

    const trigger = screen.getByRole("button", { name: "token 用量明细" });
    const metaContainer = trigger.parentElement!.parentElement!;
    const tokens = metaContainer.className.split(/\s+/);

    expect(tokens).not.toContain("opacity-0");
    expect(tokens).not.toContain("opacity-100");
    expect(metaContainer.className).not.toMatch(/transition-opacity/);
    expect(metaContainer.className).not.toMatch(/group-hover/);
    expect(metaContainer.className).not.toMatch(/focus-visible/);
    expect(metaContainer.className).toContain("text-subtle-foreground");
  });

  it("renders the content column under max-w-[720px] inside the article", () => {
    // 历史上是 760px;统一与 ToolCall / ApprovalGate / ErrorCard 一致为 720px,
    // 避免三种 max-w 在 transcript 里错位形成阶梯式 dead space。
    render(
      <ChatTranscript
        agentColor="agent-1"
        agentName="CEO 助手"
        messages={[assistantWithUsage()]}
        onRerun={() => undefined}
      />,
    );

    const trigger = screen.getByRole("button", { name: "token 用量明细" });
    const contentColumn = trigger.parentElement!.parentElement!.parentElement!;

    expect(contentColumn.tagName).toBe("DIV");
    expect(contentColumn.className).toMatch(/max-w-\[720px\]/);
    expect(contentColumn.parentElement!.tagName).toBe("ARTICLE");
  });

  it("labels the rerun action as 重新生成 and passes the target message id to onRerun", () => {
    const calls: number[] = [];
    render(
      <ChatTranscript
        agentColor="agent-1"
        agentName="CEO 助手"
        messages={[assistantWithUsage()]}
        onRerun={(messageId) => calls.push(messageId)}
      />,
    );

    expect(screen.queryByText("重跑")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /重新生成/ }));
    expect(calls).toEqual([7]);
  });

  it("renders 重新生成 on every assistant message, not just the last one", () => {
    // 历史中段也要能重跑：上一轮设计只在最后一条挂按钮，现在每条都要。
    const olderAssistant = {
      ...assistantWithUsage(),
      id: 3,
      seq: 1,
    } as chat_svc.ChatMessage;
    const newerAssistant = {
      ...assistantWithUsage(),
      id: 9,
      seq: 3,
    } as chat_svc.ChatMessage;

    const clicks: number[] = [];
    render(
      <ChatTranscript
        agentColor="agent-1"
        agentName="CEO 助手"
        messages={[olderAssistant, newerAssistant]}
        onRerun={(messageId) => clicks.push(messageId)}
      />,
    );

    const buttons = screen.getAllByRole("button", { name: /重新生成/ });
    expect(buttons).toHaveLength(2);
    fireEvent.click(buttons[0]);
    fireEvent.click(buttons[1]);
    expect(clicks).toEqual([3, 9]);
  });

  // claude/codex 后端走 CLI 自身 login（llmProviderKey 为空）或绑了 provider 但 Model
  // 字段留空时，落库的 assistantMsg.Model 是空串。之前 chat.tsx 用 `m.model` 作
  // 门槛把整个 meta 行藏掉，连耗时和「重新生成」按钮一起没了。门槛改成
  // durationMs > 0（turn 完成的可靠信号）后这些会话也能正常显示 meta。
  it("shows the meta row with rerun button when model is empty but the turn completed", () => {
    const noModelAssistant = {
      ...assistantWithUsage(),
      model: "",
    } as chat_svc.ChatMessage;

    render(
      <ChatTranscript
        agentColor="agent-1"
        agentName="CEO 助手"
        messages={[noModelAssistant]}
        onRerun={() => undefined}
      />,
    );

    expect(
      screen.getByRole("button", { name: /重新生成/ }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "token 用量明细" }),
    ).toHaveTextContent("1.2s");
  });

  it("hides the model chip text when model is empty so no empty span shows", () => {
    const noModelAssistant = {
      ...assistantWithUsage(),
      model: "",
    } as chat_svc.ChatMessage;

    render(
      <ChatTranscript
        agentColor="agent-1"
        agentName="CEO 助手"
        messages={[noModelAssistant]}
        onRerun={() => undefined}
      />,
    );

    const trigger = screen.getByRole("button", { name: "token 用量明细" });
    expect(trigger).not.toHaveTextContent("claude-sonnet-4-6");
    // 第一个 token chip 应该紧贴左边、不带 leading 「·」 分隔符
    expect(trigger.textContent ?? "").not.toMatch(/^\s*·/);
  });
});

describe("ChatTranscript typing indicator", () => {
  function userMessage(id: number, text: string): chat_svc.ChatMessage {
    return {
      blocks: [{ type: "text", text } as ChatBlockData],
      completionTokens: 0,
      createtime: new Date("2026-05-18T10:00:00Z").getTime(),
      durationMs: 0,
      errorText: "",
      id,
      model: "",
      promptTokens: 0,
      role: "user",
      seq: id,
      sessionId: 1,
    } as chat_svc.ChatMessage;
  }

  function assistantMessage(
    id: number,
    blocks: ChatBlockData[],
  ): chat_svc.ChatMessage {
    return {
      blocks,
      completionTokens: 0,
      createtime: new Date("2026-05-18T10:00:00Z").getTime(),
      durationMs: 0,
      errorText: "",
      id,
      model: "",
      promptTokens: 0,
      role: "assistant",
      seq: id,
      sessionId: 1,
    } as chat_svc.ChatMessage;
  }

  it("shows the indicator on the empty assistant placeholder when streaming", () => {
    render(
      <ChatTranscript
        agentColor="agent-1"
        agentName="CEO 助手"
        messages={[userMessage(1, "hi"), assistantMessage(2, [])]}
        streaming
      />,
    );

    expect(screen.getByLabelText("正在生成")).toBeInTheDocument();
  });

  it("places the indicator after the live tail text in DOM order", () => {
    render(
      <ChatTranscript
        agentColor="agent-1"
        agentName="CEO 助手"
        liveDelta="streaming chunk"
        liveTargetId={2}
        messages={[userMessage(1, "hi"), assistantMessage(2, [])]}
        streaming
      />,
    );

    const indicator = screen.getByLabelText("正在生成");
    const tail = screen.getByText("streaming chunk");
    expect(
      tail.compareDocumentPosition(indicator) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("does not render the indicator when streaming is false", () => {
    render(
      <ChatTranscript
        agentColor="agent-1"
        agentName="CEO 助手"
        messages={[userMessage(1, "hi"), assistantMessage(2, [])]}
      />,
    );

    expect(screen.queryByLabelText("正在生成")).not.toBeInTheDocument();
  });

  it("does not render the indicator when the trailing message is a user one", () => {
    render(
      <ChatTranscript
        agentColor="agent-1"
        agentName="CEO 助手"
        messages={[
          assistantMessage(1, [
            { type: "text", text: "old reply" } as ChatBlockData,
          ]),
          userMessage(2, "follow up"),
        ]}
        streaming
      />,
    );

    expect(screen.queryByLabelText("正在生成")).not.toBeInTheDocument();
  });

  it("renders the indicator only on the last assistant message", () => {
    render(
      <ChatTranscript
        agentColor="agent-1"
        agentName="CEO 助手"
        messages={[
          assistantMessage(1, [
            { type: "text", text: "first" } as ChatBlockData,
          ]),
          assistantMessage(2, [
            { type: "text", text: "second" } as ChatBlockData,
          ]),
        ]}
        streaming
      />,
    );

    const indicators = screen.getAllByLabelText("正在生成");
    expect(indicators).toHaveLength(1);
    const second = screen.getByText("second");
    expect(second.closest("article")).toContainElement(indicators[0]);
  });
});

describe("ChatTranscript thinking blocks", () => {
  function assistantMsg(
    id: number,
    blocks: ChatBlockData[],
  ): chat_svc.ChatMessage {
    return {
      blocks,
      completionTokens: 0,
      createtime: new Date("2026-05-18T10:00:00Z").getTime(),
      durationMs: 0,
      errorText: "",
      id,
      model: "",
      promptTokens: 0,
      role: "assistant",
      seq: id,
      sessionId: 1,
    } as chat_svc.ChatMessage;
  }

  it("renders persisted thinking block as done-collapsed (header only)", () => {
    const reasoning = "Plan: check A then B.";
    render(
      <ChatTranscript
        agentColor="agent-1"
        agentName="CEO 助手"
        messages={[
          assistantMsg(1, [
            { text: reasoning, type: "thinking" } as ChatBlockData,
            { type: "text", text: "结果是 42" } as ChatBlockData,
          ]),
        ]}
      />,
    );

    expect(screen.getByText("思考完成")).toBeInTheDocument();
    expect(screen.getByText(`· ${reasoning.length} 字`)).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "切换思考完成展开/收起" }),
    ).toHaveAttribute("aria-expanded", "false");
  });

  it("renders liveThinking as a streaming thinking card on the live target", () => {
    render(
      <ChatTranscript
        agentColor="agent-1"
        agentName="CEO 助手"
        liveThinking="正在分析问题…"
        liveTargetId={2}
        messages={[assistantMsg(2, [])]}
        streaming
      />,
    );

    expect(screen.getByText("思考中…")).toBeInTheDocument();
    expect(screen.getByText("正在分析问题…")).toBeInTheDocument();
  });

  it("renders liveThinking before liveBlocks (tool cards) in DOM order", () => {
    // 防御回归:Anthropic 协议里 thinking 永远在 turn 开头,但 store 把 liveThinking
    // 当成一个游离字段。早期实现把它 push 到 items 末尾,造成本轮一旦触发了 tool_use,
    // 思考卡片就被挤到工具卡之后,出现「思考 still 14s,工具却已经在上面」的视觉错乱。
    render(
      <ChatTranscript
        agentColor="agent-1"
        agentName="CEO 助手"
        liveBlocks={[
          {
            toolInput: { path: "." },
            toolName: "ls",
            toolUseId: "call_x",
            type: "tool_use",
          } as ChatBlockData,
        ]}
        liveThinking="先看一下目录结构"
        liveTargetId={2}
        messages={[assistantMsg(2, [])]}
        streaming
      />,
    );

    // tool_use 已经进入 liveBlocks → 思考阶段算结束,文案是「思考完成」。
    const thinking = screen.getByText("思考完成");
    // Plan C 后,非 canonical 工具走 RawToolCard(data-testid=raw-tool-card)。
    const tool = screen.getByTestId("raw-tool-card");
    expect(
      thinking.compareDocumentPosition(tool) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("liveThinking collapses to done when text deltas start (liveDelta non-empty)", () => {
    render(
      <ChatTranscript
        agentColor="agent-1"
        agentName="CEO 助手"
        liveDelta="结果是"
        liveThinking="正在分析问题…"
        liveTargetId={2}
        messages={[assistantMsg(2, [])]}
        streaming
      />,
    );

    // Thinking header collapsed to '思考完成', not '思考中…'
    expect(screen.getByText("思考完成")).toBeInTheDocument();
    expect(screen.queryByText("思考中…")).not.toBeInTheDocument();
    // Live text appears
    expect(screen.getByText("结果是")).toBeInTheDocument();
  });

  it("liveThinking collapses to done when a tool_use enters liveBlocks (even before any text delta)", () => {
    // Regression: 早期实现只用 !liveTail 判定 streaming,导致「思考完成 → 直接发起一个 Bash
    // 工具」的瞬间(liveBlocks 已经有 tool_use 但 liveDelta 还是空)思考徽标一直 pulse、计时
    // 卡住不动。tool_use 本身已经是「思考之后的输出」,理应把思考收为「思考完成」。
    render(
      <ChatTranscript
        agentColor="agent-1"
        agentName="CEO 助手"
        liveBlocks={[
          {
            toolInput: { command: "ls" },
            toolName: "Bash",
            toolUseId: "call_x",
            type: "tool_use",
          } as ChatBlockData,
        ]}
        liveThinking="先看一下目录结构"
        liveTargetId={2}
        messages={[assistantMsg(2, [])]}
        streaming
      />,
    );

    expect(screen.getByText("思考完成")).toBeInTheDocument();
    expect(screen.queryByText("思考中…")).not.toBeInTheDocument();
  });
});

describe("ChatTranscript subagent blocks", () => {
  it("marks subagent header text as copyable without making selection clicks expand it", () => {
    const card = renderTranscriptWithSubagent();
    const toggle = within(card).getAllByRole("button")[0];
    const description = within(toggle).getByText("probe");
    const textNode = description.firstChild;
    if (!textNode) throw new Error("Expected subagent description text node");
    const selection = mockTextSelectionWithin(textNode);

    expect(within(toggle).getByText("Agent")).toHaveAttribute(
      "data-copyable-control-text",
      "true",
    );
    expect(description).toHaveAttribute("data-copyable-control-text", "true");

    fireEvent.click(toggle);

    expect(toggle).toHaveAttribute("aria-expanded", "false");
    selection.mockRestore();
  });

  it("renders Agent tool as SubagentInvocationCard, hides child blocks from top level", () => {
    const card = renderTranscriptWithSubagent();
    // 头部是一行：Agent · probe + general-purpose chip + tool 计数 + DONE。
    // last 工具名已从 header 去掉(只保留计数),避免一行过长。
    expect(within(card).getByText("Agent")).toBeInTheDocument();
    expect(within(card).getByText("probe")).toBeInTheDocument();
    expect(within(card).getByText("general-purpose")).toBeInTheDocument();
    expect(within(card).getByText(/^1 tool$/)).toBeInTheDocument();
    expect(within(card).queryByText(/last:/)).toBeNull();
    expect(within(card).getByText(/DONE · 7\.8s/)).toBeInTheDocument();

    // 子 Bash 不应出现在与 Agent 同级的位置 —— 没有独立的 Bash 工具卡。
    expect(screen.queryByRole("region", { name: "工具调用 Bash" })).toBeNull();
  });

  it("expanded card lists subagent inner Bash step + final summary", () => {
    const card = renderTranscriptWithSubagent();
    fireEvent.click(within(card).getAllByRole("button")[0]);

    expect(within(card).getByText("TASK PROMPT")).toBeInTheDocument();
    expect(within(card).getByText("STEPS")).toBeInTheDocument();
    expect(within(card).getByText("SUMMARY")).toBeInTheDocument();
    expect(
      within(card).getByText("Run echo hello and return."),
    ).toBeInTheDocument();
    // Bash 子步骤的 header 出现在 STEPS 区
    expect(within(card).getByText("Bash")).toHaveAttribute(
      "data-copyable-control-text",
      "true",
    );
    expect(within(card).getByText("echo hello")).toHaveAttribute(
      "data-copyable-control-text",
      "true",
    );
    // SUMMARY 区有最终文本
    expect(within(card).getByText(/Raw output:/)).toBeInTheDocument();
  });

  // Plan C: AgentSpawnCard 不读 toolInput.model — canonical.AgentSpawn 没有 model 字段。
  // 旧 SubagentInvocationCard 渲染 model chip 的逻辑已废除;前端从 canonical 取数据,
  // model 不在 wire 里就不显示。
});

describe("ChatTranscript permission + tool merge", () => {
  it("marks standalone permission summary text as copyable while keeping action buttons plain", () => {
    render(
      <ChatTranscript
        agentColor="agent-1"
        agentName="CEO 助手"
        messages={[
          chatMessage([
            {
              canonical: {
                kind: "tool.permission",
                toolPermission: {
                  requestId: "req-copy",
                  toolName: "Bash",
                  toolInput: { command: "printf hi" },
                  resolved: false,
                },
              },
              toolPermission: {
                requestId: "req-copy",
                toolName: "Bash",
                toolInput: { command: "printf hi" },
                resolved: false,
              },
              type: "tool_permission_request",
            } as unknown as ChatBlockData,
          ]),
        ]}
      />,
    );

    expect(screen.getByText("Bash")).toHaveAttribute(
      "data-copyable-control-text",
      "true",
    );
    expect(screen.getByText("printf hi")).toHaveAttribute(
      "data-copyable-control-text",
      "true",
    );
    expect(screen.getByText("仅本次允许")).not.toHaveAttribute(
      "data-copyable-control-text",
    );
  });

  // Plan C: "merges resolved+allowed permission into the next matching tool_use card" +
  // "uses 'Allowed · session' badge when alwaysAllow=true" 两条特性化 ToolInvocationCard
  // header 上 Allowed badge 的测试已删除 —— 新 canonical-tool/raw/card.tsx 简化为
  // 只显示 toolName + 摘要 + 可选 overlay,不再挂 inline badge(审批信息保留在 toolBlock.
  // toolPermission sidecar,后续 RawToolCard 自行决定如何展示)。

  it("keeps denied permissions as a standalone card with no following tool_use", () => {
    render(
      <ChatTranscript
        agentColor="agent-1"
        agentName="CEO 助手"
        messages={[
          chatMessage([
            {
              canonical: {
                kind: "tool.permission",
                toolPermission: {
                  requestId: "req-3",
                  toolName: "Bash",
                  toolInput: { command: "rm -rf /" },
                  resolved: true,
                  allowed: false,
                  alwaysAllow: false,
                },
              },
              toolPermission: {
                requestId: "req-3",
                toolName: "Bash",
                toolInput: { command: "rm -rf /" },
                resolved: true,
                allowed: false,
                alwaysAllow: false,
              },
              type: "tool_permission_request",
            } as unknown as ChatBlockData,
          ]),
        ]}
      />,
    );

    // ToolPermissionCard 仍渲染 (only header 显示 toolName 和 已拒绝 pill)
    expect(screen.getByText("已拒绝")).toBeInTheDocument();
    // 没有 tool_use 卡
    expect(screen.queryByRole("region", { name: "工具调用 Bash" })).toBeNull();
  });

  it("keeps pending (unresolved) permissions as a standalone card", () => {
    render(
      <ChatTranscript
        agentColor="agent-1"
        agentName="CEO 助手"
        messages={[
          chatMessage([
            {
              canonical: {
                kind: "tool.permission",
                toolPermission: {
                  requestId: "req-4",
                  toolName: "Bash",
                  toolInput: { command: "ls" },
                  resolved: false,
                },
              },
              toolPermission: {
                requestId: "req-4",
                toolName: "Bash",
                toolInput: { command: "ls" },
                resolved: false,
              },
              type: "tool_permission_request",
            } as unknown as ChatBlockData,
          ]),
        ]}
      />,
    );

    // 待审批态留三个操作按钮,confirm 卡片确实出现。
    expect(screen.getByText("仅本次允许")).toBeInTheDocument();
    expect(screen.getByText("本会话始终允许")).toBeInTheDocument();
    expect(screen.getByText("拒绝")).toBeInTheDocument();
  });
});

describe("ChatTranscript hides AskUserQuestion tool_use", () => {
  it("does not render a tool card for AskUserQuestion's tool_use / tool_result", () => {
    render(
      <ChatTranscript
        agentColor="agent-1"
        agentName="CEO 助手"
        messages={[
          chatMessage([
            {
              askUserQuestion: {
                requestId: "ask-1",
                questions: [
                  {
                    question: "选哪个?",
                    multiSelect: false,
                    options: [
                      { label: "A", description: "" },
                      { label: "B", description: "" },
                    ],
                  },
                ],
                answered: true,
                answers: [{ questionIndex: 0, labels: ["A"], otherText: "" }],
              },
              // Plan C: backend live + replay 路径都填 canonical.UserAsk;前端走
              // CanonicalToolRouter → UserAskCard 渲染。
              canonical: {
                kind: "user.ask",
                userAsk: {
                  requestId: "ask-1",
                  questions: [
                    {
                      question: "选哪个?",
                      multiSelect: false,
                      options: [
                        { label: "A", description: "" },
                        { label: "B", description: "" },
                      ],
                    },
                  ],
                  answered: true,
                  answers: [{ questionIndex: 0, labels: ["A"], otherText: "" }],
                },
              },
              type: "ask_user_question",
            } as ChatBlockData,
            {
              toolInput: { questions: [] },
              toolName: "AskUserQuestion",
              toolUseId: "auq-1",
              type: "tool_use",
            } as ChatBlockData,
            {
              text: "ok",
              toolUseId: "auq-1",
              type: "tool_result",
            } as ChatBlockData,
          ]),
        ]}
      />,
    );

    // UserAskCard 渲染(canonical-tool/user-ask;header 显示 user_ask)
    expect(screen.getByTestId("user-ask-card")).toBeInTheDocument();
    expect(screen.getByText("user_ask")).toBeInTheDocument();
    // 不存在 AskUserQuestion 的独立 tool_use 卡片
    expect(
      screen.queryByRole("region", { name: /工具调用 AskUserQuestion/ }),
    ).toBeNull();
  });
});

// ExitPlanMode 的 PlanApproveCard 已经承担了"批准执行计划"的完整渲染,后续 CLI
// 真正调用 ExitPlanMode 时同样会冒出一条 tool_use(及配对 tool_result),如果按
// 通用 tool_use 路径渲染会得到一张"裸 ExitPlanMode"卡夹在 PlanApproveCard 旁边,
// 视觉重复。这里参照 AskUserQuestion 的做法,在 consumeBlock 里直接 skip。
describe("ChatTranscript hides ExitPlanMode tool_use", () => {
  it("renders only PlanApproveCard, no separate tool_use card for ExitPlanMode", () => {
    render(
      <ChatTranscript
        agentColor="agent-1"
        agentName="CEO 助手"
        messages={[
          chatMessage([
            {
              canonical: {
                kind: "plan.approve_request",
                planApprove: {
                  requestId: "perm-plan-1",
                  planText: "## Plan\n- step a\n- step b",
                  resolved: true,
                  allowed: true,
                },
              },
              toolPermission: {
                requestId: "perm-plan-1",
                toolName: "ExitPlanMode",
                toolInput: { plan: "## Plan\n- step a\n- step b" },
                resolved: true,
                allowed: true,
              },
              type: "tool_permission_request",
            } as unknown as ChatBlockData,
            {
              toolInput: { plan: "## Plan\n- step a\n- step b" },
              toolName: "ExitPlanMode",
              toolUseId: "epm-1",
              type: "tool_use",
            } as ChatBlockData,
            {
              text: "",
              toolUseId: "epm-1",
              type: "tool_result",
            } as ChatBlockData,
          ]),
        ]}
      />,
    );

    expect(screen.getByTestId("plan-card")).toBeInTheDocument();
    // ExitPlanMode 没有独立 tool_use 卡
    expect(
      screen.queryByRole("region", { name: /工具调用 ExitPlanMode/ }),
    ).toBeNull();
    // 也不应出现 RawToolCard 把 toolName="ExitPlanMode" 暴露出来。
    expect(screen.queryByText("ExitPlanMode")).toBeNull();
  });
});

describe("ChatTranscript plan.update rendering", () => {
  it("does not render synthetic type=plan blocks in the transcript", () => {
    render(
      <ChatTranscript
        agentColor="agent-1"
        agentName="CEO 助手"
        messages={[
          chatMessage([
            {
              text: "# Plan\n\n1. Inspect\n2. Test",
              type: "plan",
            } as ChatBlockData,
          ]),
        ]}
      />,
    );

    expect(screen.queryByTestId("plan-card")).toBeNull();
    expect(screen.queryByText("Inspect")).toBeNull();
  });

  it("renders Codex plan-mode type=plan blocks with actions as a plan card", () => {
    render(
      <ChatTranscript
        agentColor="agent-1"
        agentName="CEO 助手"
        sessionId={7}
        messages={[
          chatMessage([
            {
              toolInput: { command: "echo before plan" },
              toolName: "command_execution",
              toolUseId: "bash-1",
              type: "tool_use",
            } as ChatBlockData,
            {
              text: "ok",
              toolUseId: "bash-1",
              type: "tool_result",
            } as ChatBlockData,
            {
              canonical: {
                kind: "plan.update",
                planUpdate: {
                  text: "# Plan\n\n1. Inspect\n2. Test",
                  actions: [
                    { id: "plan.execute", kind: "approve" },
                    {
                      id: "plan.refine",
                      kind: "refine",
                      requiresFeedback: true,
                    },
                  ],
                  steps: [],
                },
              },
              text: "# Plan\n\n1. Inspect\n2. Test",
              type: "plan",
            } as unknown as ChatBlockData,
          ]),
        ]}
      />,
    );

    expect(screen.getByTestId("plan-card")).toBeInTheDocument();
    expect(screen.getByText("执行计划")).toBeInTheDocument();
    expect(screen.getByText("继续完善")).toBeInTheDocument();
  });

  it("renders plan.update tool_use as an ordinary raw tool card", () => {
    render(
      <ChatTranscript
        agentColor="agent-1"
        agentName="CEO 助手"
        messages={[
          chatMessage([
            {
              canonical: {
                kind: "plan.update",
                planUpdate: {
                  steps: [
                    { step: "Inspect", status: "completed" },
                    { step: "Test", status: "inProgress" },
                  ],
                },
              },
              toolInput: { plan: "- [x] Inspect\n- [ ] Test" },
              toolName: "update_plan",
              toolUseId: "plan-1",
              type: "tool_use",
            } as unknown as ChatBlockData,
            {
              text: "ok",
              toolUseId: "plan-1",
              type: "tool_result",
            } as ChatBlockData,
          ]),
        ]}
      />,
    );

    expect(screen.getByTestId("raw-tool-card")).toBeInTheDocument();
    expect(screen.queryByTestId("plan-card")).toBeNull();
    expect(screen.getByText("update_plan")).toBeInTheDocument();
    expect(screen.queryByText("plan.update")).toBeNull();
    expect(screen.getByText("DONE")).toBeInTheDocument();
  });
});

describe("ChatTranscript compact_boundary fold", () => {
  function makeMessage(
    id: number,
    role: "user" | "assistant",
    blocks: ChatBlockData[],
  ): chat_svc.ChatMessage {
    return {
      blocks,
      cachedTokens: 0,
      cacheCreationTokens: 0,
      completionTokens: 0,
      createtime: new Date("2026-05-27T10:00:00Z").getTime(),
      durationMs: 0,
      errorText: "",
      id,
      model: "",
      promptTokens: 0,
      reasoningTokens: 0,
      role,
      seq: id,
      sessionId: 1,
      totalInputTokens: 0,
    } as unknown as chat_svc.ChatMessage;
  }

  it("折叠 boundary 之前的消息,显示展开按钮 + 边界 divider", () => {
    render(
      <ChatTranscript
        agentColor="agent-1"
        agentName="A"
        messages={[
          makeMessage(1, "user", [{ type: "text", text: "old-question" }]),
          makeMessage(2, "assistant", [{ type: "text", text: "old-answer" }]),
          makeMessage(3, "assistant", [
            {
              type: "compact_boundary",
              compact: { preTokens: 12345, trigger: "auto", at: 0 },
            } as unknown as ChatBlockData,
            { type: "text", text: "fresh-answer" },
          ]),
        ]}
      />,
    );

    expect(screen.queryByText("old-question")).toBeNull();
    expect(screen.queryByText("old-answer")).toBeNull();
    expect(screen.getByText("fresh-answer")).toBeInTheDocument();
    expect(screen.getByText("上下文已压缩")).toBeInTheDocument();
    // 折叠条:文案"查看压缩前的 2 条消息"
    const expandBtn = screen.getByRole("button", {
      name: /查看压缩前的 2 条消息/,
    });
    expect(expandBtn).toBeInTheDocument();
  });

  it("点击展开按钮后旧消息全部可见", () => {
    render(
      <ChatTranscript
        agentColor="agent-1"
        agentName="A"
        messages={[
          makeMessage(1, "user", [{ type: "text", text: "old-question" }]),
          makeMessage(2, "assistant", [
            {
              type: "compact_boundary",
              compact: { trigger: "manual", at: 0 },
            } as unknown as ChatBlockData,
            { type: "text", text: "fresh-answer" },
          ]),
        ]}
      />,
    );

    expect(screen.queryByText("old-question")).toBeNull();
    fireEvent.click(
      screen.getByRole("button", { name: /查看压缩前的 1 条消息/ }),
    );
    expect(screen.getByText("old-question")).toBeInTheDocument();
    expect(screen.getByText("fresh-answer")).toBeInTheDocument();
  });

  it("没有 compact_boundary 时不折叠 / 不显示按钮", () => {
    render(
      <ChatTranscript
        agentColor="agent-1"
        agentName="A"
        messages={[
          makeMessage(1, "user", [{ type: "text", text: "q" }]),
          makeMessage(2, "assistant", [{ type: "text", text: "a" }]),
        ]}
      />,
    );

    expect(screen.getByText("q")).toBeInTheDocument();
    expect(screen.getByText("a")).toBeInTheDocument();
    expect(screen.queryByText("上下文已压缩")).toBeNull();
    expect(screen.queryByRole("button", { name: /查看压缩前/ })).toBeNull();
  });
});
