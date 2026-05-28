import { act, render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useChatStreamsStore } from "@/stores/chat-streams-store";
import { useChatTabsStore } from "@/stores/chat-tabs-store";
import { useSessionStatusStore } from "@/stores/session-status-store";

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
  runtimeMocks.EventsOn.mockReset();
  runtimeMocks.EventsOn.mockImplementation(() => vi.fn());
}

function registeredHandler(): (ev: ChatStreamEvent) => void {
  const calls = runtimeMocks.EventsOn.mock.calls as unknown as Array<
    [string, (ev: ChatStreamEvent) => void]
  >;
  return calls[0][1];
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
      useChatStreamsStore.getState().streams.get(42)?.liveContextWindow,
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
    useChatStreamsStore.getState().appendLiveText(42, "before-compact");

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

    const stream = useChatStreamsStore.getState().streams.get(42);
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
    expect(useChatStreamsStore.getState().streams.get(42)?.liveCompacting).toBe(
      false,
    );

    // CLI 通报 compacting 开始。
    act(() => {
      handler({
        kind: "runtime_status",
        runtimeStatus: { status: "compacting", compacting: true },
      });
    });
    expect(useChatStreamsStore.getState().streams.get(42)?.liveCompacting).toBe(
      true,
    );

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
    expect(useChatStreamsStore.getState().streams.get(42)?.liveCompacting).toBe(
      false,
    );
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
    expect(useChatStreamsStore.getState().streams.get(42)?.liveCompacting).toBe(
      false,
    );
  });
});
