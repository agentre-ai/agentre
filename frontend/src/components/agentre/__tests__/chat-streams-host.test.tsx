import { act, render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  streamForMessage,
  useChatStreamsStore,
} from "@/stores/chat-streams-store";
import { useChatTabsStore } from "@/stores/chat-tabs-store";
import { useSessionConnStore } from "@/stores/session-conn-store";
import { useSessionStatusStore } from "@/stores/session-status-store";

import {
  __resetCatchUpStateForTesting,
  getCatchUp,
} from "../chat-panel-catchup-state";
import { ChatStreamsHost } from "../chat-streams-host";

import type { ChatStreamEvent } from "@/hooks/use-chat-stream";

const runtimeMocks = vi.hoisted(() => ({
  EventsOn: vi.fn(() => vi.fn()),
}));

vi.mock("../../../../wailsjs/runtime/runtime", () => runtimeMocks);

function resetStores() {
  useChatStreamsStore.setState({ streams: new Map() });
  useChatTabsStore.setState({ tabs: [], activeTabId: null });
  useSessionStatusStore.getState().__reset();
  useSessionConnStore.getState().__reset();
  __resetCatchUpStateForTesting();
  runtimeMocks.EventsOn.mockReset();
  runtimeMocks.EventsOn.mockImplementation(() => vi.fn());
}

function eventsOnCalls(): Array<[string, (ev: ChatStreamEvent) => void]> {
  return runtimeMocks.EventsOn.mock.calls as unknown as Array<
    [string, (ev: ChatStreamEvent) => void]
  >;
}

function registeredHandler(): (ev: ChatStreamEvent) => void {
  return eventsOnCalls()[0][1];
}

function handlerFor(streamName: string): (ev: ChatStreamEvent) => void {
  const call = eventsOnCalls().find(([name]) => name === streamName);
  if (!call) throw new Error(`no EventsOn subscription for ${streamName}`);
  return call[1];
}

describe("ChatStreamsHost", () => {
  beforeEach(() => {
    resetStores();
  });

  it("Given an open tab behind others, When a tool permission event arrives, Then the tab moves after pinned tabs", async () => {
    useChatTabsStore.getState().openSessionInNewTab(1);
    useChatTabsStore.getState().openSessionInNewTab(2);
    useChatTabsStore.getState().openSessionInNewTab(42);
    const pinnedId = useChatTabsStore.getState().tabs[0].id;
    useChatTabsStore.getState().togglePin(pinnedId);
    useChatStreamsStore.getState().openStream({
      assistantMessageId: 1001,
      name: "chat:event:42:1001",
      sessionId: 42,
      streamStartedAt: Date.now(),
    });

    render(<ChatStreamsHost />);

    await waitFor(() => expect(runtimeMocks.EventsOn).toHaveBeenCalled());
    const handler = registeredHandler();

    act(() => {
      handler({
        kind: "tool_permission_request",
        toolPermission: {
          requestId: "perm-1",
          toolName: "Bash",
          toolInput: {},
          resolved: false,
        },
      });
    });

    expect(
      useChatTabsStore
        .getState()
        .tabs.map((t) => (t.meta as { sessionId: number }).sessionId),
    ).toEqual([1, 42, 2]);
  });

  it("applies contextWindow-only session_status patches to the live stream without clearing status", async () => {
    useChatStreamsStore.getState().openStream({
      assistantMessageId: 1001,
      name: "chat:event:42:1001",
      sessionId: 42,
      streamStartedAt: Date.now(),
    });
    useSessionStatusStore.getState().upsert(42, {
      agentStatus: "running",
      needsAttention: false,
      permissionMode: "plan",
    });

    render(<ChatStreamsHost />);

    await waitFor(() => expect(runtimeMocks.EventsOn).toHaveBeenCalled());
    const handler = registeredHandler();

    act(() => {
      handler({
        kind: "session_status",
        sessionStatus: {
          agentStatus: "",
          needsAttention: false,
          contextWindow: 258400,
        },
      });
    });

    expect(
      streamForMessage(useChatStreamsStore.getState(), 42, 1001)
        ?.liveContextWindow,
    ).toBe(258400);
    expect(useSessionStatusStore.getState().statuses.get(42)).toMatchObject({
      agentStatus: "running",
      needsAttention: false,
      permissionMode: "plan",
    });
  });

  it("permissionMode-only session_status patches preserve the existing status fields", async () => {
    useChatStreamsStore.getState().openStream({
      assistantMessageId: 1001,
      name: "chat:event:42:1001",
      sessionId: 42,
      streamStartedAt: Date.now(),
    });
    useSessionStatusStore.getState().upsert(42, {
      agentStatus: "running",
      needsAttention: false,
      permissionMode: "plan",
    });

    render(<ChatStreamsHost />);

    await waitFor(() => expect(runtimeMocks.EventsOn).toHaveBeenCalled());
    const handler = registeredHandler();

    act(() => {
      handler({
        kind: "session_status",
        sessionStatus: {
          agentStatus: "",
          needsAttention: false,
          permissionMode: "default",
        },
      });
    });

    expect(useSessionStatusStore.getState().statuses.get(42)).toMatchObject({
      agentStatus: "running",
      needsAttention: false,
      permissionMode: "default",
    });
  });

  it("compact_boundary event appends a compact_boundary block flushing pending text", async () => {
    useChatStreamsStore.getState().openStream({
      assistantMessageId: 1001,
      name: "chat:event:42:1001",
      sessionId: 42,
      streamStartedAt: Date.now(),
    });
    // 提前在 liveDelta 累一段文本,确认 boundary 到达时被 flush 为 text block。
    useChatStreamsStore.getState().appendLiveText(42, 1001, "before-compact");

    render(<ChatStreamsHost />);

    await waitFor(() => expect(runtimeMocks.EventsOn).toHaveBeenCalled());
    const handler = registeredHandler();

    act(() => {
      handler({
        kind: "compact_boundary",
        compact: {
          messageId: 1001,
          seq: 5,
          preTokens: 12345,
          trigger: "auto",
          at: 1700000000000,
        },
      });
    });

    const stream = streamForMessage(useChatStreamsStore.getState(), 42, 1001);
    expect(stream?.liveBlocks).toHaveLength(2);
    expect(stream?.liveBlocks[0]).toMatchObject({
      type: "text",
      text: "before-compact",
    });
    expect(stream?.liveBlocks[1]).toMatchObject({
      type: "compact_boundary",
      compact: { preTokens: 12345, trigger: "auto", at: 1700000000000 },
    });
    expect(stream?.liveDelta).toBe("");
  });

  it("runtime_status compacting=true sets liveCompacting; compact_boundary clears it", async () => {
    useChatStreamsStore.getState().openStream({
      assistantMessageId: 1001,
      name: "chat:event:42:1001",
      sessionId: 42,
      streamStartedAt: Date.now(),
    });

    render(<ChatStreamsHost />);

    await waitFor(() => expect(runtimeMocks.EventsOn).toHaveBeenCalled());
    const handler = registeredHandler();

    // 起点:openStream 默认 liveCompacting=false。
    expect(
      streamForMessage(useChatStreamsStore.getState(), 42, 1001)
        ?.liveCompacting,
    ).toBe(false);

    // CLI 通报 compacting 开始。
    act(() => {
      handler({
        kind: "runtime_status",
        runtimeStatus: { status: "compacting", compacting: true },
      });
    });
    expect(
      streamForMessage(useChatStreamsStore.getState(), 42, 1001)
        ?.liveCompacting,
    ).toBe(true);

    // compact_boundary 到达即视为压缩结束 —— 自动清旗,不依赖 CLI 显式发 status:""。
    act(() => {
      handler({
        kind: "compact_boundary",
        compact: {
          messageId: 1001,
          seq: 0,
          preTokens: 30000,
          trigger: "manual",
          at: 1700000000000,
        },
      });
    });
    expect(
      streamForMessage(useChatStreamsStore.getState(), 42, 1001)
        ?.liveCompacting,
    ).toBe(false);
  });

  it("runtime_status with non-compacting status does not flip compacting flag", async () => {
    useChatStreamsStore.getState().openStream({
      assistantMessageId: 1001,
      name: "chat:event:42:1001",
      sessionId: 42,
      streamStartedAt: Date.now(),
    });

    render(<ChatStreamsHost />);

    await waitFor(() => expect(runtimeMocks.EventsOn).toHaveBeenCalled());
    const handler = registeredHandler();

    act(() => {
      handler({
        kind: "runtime_status",
        runtimeStatus: { status: "requesting", compacting: false },
      });
    });
    expect(
      streamForMessage(useChatStreamsStore.getState(), 42, 1001)
        ?.liveCompacting,
    ).toBe(false);
  });

  it("permission-mode-only session_status preserves existing bgRunning=true (no spurious clear)", async () => {
    useChatStreamsStore.getState().openStream({
      assistantMessageId: 1001,
      name: "chat:event:42:1001",
      sessionId: 42,
      streamStartedAt: Date.now(),
    });
    // Seed store: session 42 has a bg subagent running.
    useSessionStatusStore.getState().upsert(42, {
      agentStatus: "idle",
      needsAttention: false,
      bgRunning: true,
    });

    render(<ChatStreamsHost />);

    await waitFor(() => expect(runtimeMocks.EventsOn).toHaveBeenCalled());
    const handler = registeredHandler();

    // Simulate a permission-mode-only frame (hasStatus=false, hasMode=true, bgRunning omitted/false).
    act(() => {
      handler({
        kind: "session_status",
        sessionStatus: {
          agentStatus: "",
          needsAttention: false,
          permissionMode: "default",
          bgRunning: false,
        },
      });
    });

    // bgRunning must be preserved — frame had no agentStatus so it must not overwrite.
    expect(useSessionStatusStore.getState().statuses.get(42)?.bgRunning).toBe(
      true,
    );
  });

  it("session_status event with bgRunning:true ingests bgRunning into session-status-store", async () => {
    useChatStreamsStore.getState().openStream({
      assistantMessageId: 1001,
      name: "chat:event:42:1001",
      sessionId: 42,
      streamStartedAt: Date.now(),
    });

    render(<ChatStreamsHost />);

    await waitFor(() => expect(runtimeMocks.EventsOn).toHaveBeenCalled());
    const handler = registeredHandler();

    act(() => {
      handler({
        kind: "session_status",
        sessionStatus: {
          agentStatus: "running",
          needsAttention: false,
          bgRunning: true,
        },
      });
    });

    expect(useSessionStatusStore.getState().statuses.get(42)?.bgRunning).toBe(
      true,
    );
  });

  // subagent_model(R2/R4):后端只带 toolUseId + model 两个字段(不复用整份 Subagent
  // 快照),避免浅合并把已累计的 status/toolUses/totalTokens 覆盖成空值。这里断言
  // ChatStreamsHost 把它 merge 进对应 tool_use block 时同样只碰 model 字段。
  it("Given a subagent_model event, When it arrives, Then it merges model into the tool_use block without clearing existing progress fields", async () => {
    useChatStreamsStore.getState().openStream({
      assistantMessageId: 1001,
      name: "chat:event:42:1001",
      sessionId: 42,
      streamStartedAt: Date.now(),
    });
    useChatStreamsStore.getState().appendLiveToolUse(42, 1001, {
      toolUseId: "toolu_agent",
      toolName: "Agent",
      subagent: {
        status: "running",
        toolUses: 3,
        totalTokens: 500,
      },
    });

    render(<ChatStreamsHost />);

    await waitFor(() => expect(runtimeMocks.EventsOn).toHaveBeenCalled());
    const handler = registeredHandler();

    act(() => {
      handler({
        kind: "subagent_model",
        toolUseId: "toolu_agent",
        model: "claude-haiku-4-5-20251001",
      });
    });

    const block = streamForMessage(
      useChatStreamsStore.getState(),
      42,
      1001,
    )?.liveBlocks.find((b) => b.toolUseId === "toolu_agent");
    expect(block?.subagent).toMatchObject({
      status: "running",
      toolUses: 3,
      totalTokens: 500,
      model: "claude-haiku-4-5-20251001",
    });
  });

  // 连接态订阅必须挂在这个跨路由长存的宿主上,而不是 ChatPanel:panel 切走再切回
  // (乃至根本没打开)期间的连接态变化只发一次,挂在 panel 上就会漏掉 —— 重新
  // 挂载的转录流只能看到打字指示器,而真实情况是网断了。
  it("Given a live session, When its connection drops, Then the session-level conn stream is subscribed by the long-lived host and flips the conn store", async () => {
    useChatStreamsStore.getState().openStream({
      assistantMessageId: 1001,
      name: "chat:event:42:1001",
      sessionId: 42,
      streamStartedAt: Date.now(),
    });

    render(<ChatStreamsHost />);

    await waitFor(() =>
      expect(runtimeMocks.EventsOn).toHaveBeenCalledWith(
        "chat:conn:42",
        expect.any(Function),
      ),
    );
    const connHandler = handlerFor("chat:conn:42");

    act(() => {
      connHandler({
        kind: "connection_state",
        connectionState: "reconnecting",
      });
    });
    expect(useSessionConnStore.getState().stateOf(42)).toBe("reconnecting");

    act(() => {
      connHandler({ kind: "connection_state", connectionState: "connected" });
    });
    expect(useSessionConnStore.getState().stateOf(42)).toBe("connected");
  });

  it("Given a session whose last stream ended, When the conn subscription drops, Then no stale connection state leaks into the next turn", async () => {
    useChatStreamsStore.getState().openStream({
      assistantMessageId: 1001,
      name: "chat:event:42:1001",
      sessionId: 42,
      streamStartedAt: Date.now(),
    });

    render(<ChatStreamsHost />);
    await waitFor(() =>
      expect(runtimeMocks.EventsOn).toHaveBeenCalledWith(
        "chat:conn:42",
        expect.any(Function),
      ),
    );
    act(() => {
      handlerFor("chat:conn:42")({
        kind: "connection_state",
        connectionState: "reconnecting",
      });
    });

    act(() => {
      useChatStreamsStore.getState().finishStream(42, 1001, { kind: "done" });
    });

    await waitFor(() =>
      expect(useSessionConnStore.getState().stateOf(42)).toBe("connected"),
    );
  });

  // R14 的上游一半:补齐落定那一发带着「重放了几条 / 还剩几个待决策」。它必须
  // 在这个长存宿主上被记下来 —— 补齐可能发生在用户翻着历史、甚至切走路由的时候,
  // 记在会随 tab 销毁的 panel 上就等于没记,用户回到转录区只看到内容凭空多了一截。
  it("Given the channel comes back after a catch-up, When the connected frame lands, Then the host records the new-item and pending-decision counts", async () => {
    useChatStreamsStore.getState().openStream({
      assistantMessageId: 1001,
      name: "chat:event:42:1001",
      sessionId: 42,
      streamStartedAt: Date.now(),
    });

    render(<ChatStreamsHost />);
    await waitFor(() =>
      expect(runtimeMocks.EventsOn).toHaveBeenCalledWith(
        "chat:conn:42",
        expect.any(Function),
      ),
    );

    act(() => {
      handlerFor("chat:conn:42")({
        kind: "connection_state",
        connectionState: "connected",
        caughtUpCount: 12,
        pendingDecisions: 1,
      });
    });

    expect(getCatchUp(42)).toEqual({ newItems: 12, pendingDecisions: 1 });
  });

  it("Given the channel merely drops, When the reconnecting frame lands, Then no catch-up summary is recorded", async () => {
    useChatStreamsStore.getState().openStream({
      assistantMessageId: 1001,
      name: "chat:event:42:1001",
      sessionId: 42,
      streamStartedAt: Date.now(),
    });

    render(<ChatStreamsHost />);
    await waitFor(() =>
      expect(runtimeMocks.EventsOn).toHaveBeenCalledWith(
        "chat:conn:42",
        expect.any(Function),
      ),
    );

    act(() => {
      handlerFor("chat:conn:42")({
        kind: "connection_state",
        connectionState: "reconnecting",
      });
    });

    expect(getCatchUp(42)).toBeNull();
  });
});
