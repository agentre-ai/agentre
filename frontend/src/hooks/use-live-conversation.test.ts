import { renderHook, act } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

const openStreamMock = vi.fn();
const reloadMock = vi.fn();
let autonomousHandler: ((ev: unknown) => void) | null = null;
let liveStream: unknown = null;
let doneTick = 0;

vi.mock("@/hooks/use-chat-session", () => ({
  useChatSession: () => ({
    session: {
      backendType: "claudecode",
      contextWindow: 0,
      permissionMode: "default",
    },
    messages: [{ id: 1, role: "user" }],
    setMessages: vi.fn(),
    reload: reloadMock,
    error: null,
  }),
}));
vi.mock("@/hooks/use-chat-stream", () => ({
  useChatStream: (_name: string | null, handler: (ev: unknown) => void) => {
    autonomousHandler = handler;
  },
}));
vi.mock("@/stores/chat-streams-store", () => ({
  useChatStreamsStore: Object.assign(
    (sel: (s: unknown) => unknown) =>
      sel({
        streams: new Map(liveStream ? [[7, liveStream]] : []),
        openStream: openStreamMock,
      }),
    { getState: () => ({ openStream: openStreamMock }) },
  ),
}));
vi.mock("@/stores/session-status-store", () => ({
  markSessionRunning: vi.fn(),
  useSessionStatusStore: (sel: (s: unknown) => unknown) =>
    sel({ statuses: new Map([[7, { doneTick }]]) }),
}));
vi.mock("./use-composer-send", () => ({
  useComposerSend: () => ({
    submit: vi.fn(),
    sending: false,
    error: null,
    backendType: "claudecode",
    isModeSwitchable: true,
    supportsImageInput: true,
    permissionMode: { mode: "default" },
    permissionModeMeta: { order: [] },
  }),
}));

import { useLiveConversation } from "./use-live-conversation";

describe("useLiveConversation", () => {
  beforeEach(() => {
    openStreamMock.mockReset();
    reloadMock.mockReset();
    autonomousHandler = null;
    liveStream = null;
    doneTick = 0;
  });

  it("收到 autonomous_started 帧 → openStream", () => {
    renderHook(() => useLiveConversation(7, 3));
    act(() => {
      autonomousHandler?.({
        kind: "autonomous_started",
        stream: "chat:stream:7:200",
        assistantMessage: { id: 200, role: "assistant" },
      });
    });
    expect(openStreamMock).toHaveBeenCalledWith(
      expect.objectContaining({
        name: "chat:stream:7:200",
        sessionId: 7,
        assistantMessageId: 200,
      }),
    );
  });

  it("chat-streams-store 有 LiveStream 时把 live overlay 透出", () => {
    liveStream = {
      assistantMessageId: 200,
      liveDelta: "partial",
      liveThinking: "",
      liveBlocks: [],
      liveRetry: null,
      streamStartedAt: 123,
      liveCompacting: false,
    };
    const { result } = renderHook(() => useLiveConversation(7, 3));
    expect(result.current.live.liveDelta).toBe("partial");
    expect(result.current.live.streaming).toBe(true);
    // liveTargetId 必须从 stream.assistantMessageId 透出 —— ChatTranscript 用它
    // 定位「哪条 assistant 消息挂 live tail」。缺失则 buildTranscriptRows 的
    // isLive 恒 false,流式文字整块落空,编排会话就不会像对话模块那样流式输出。
    expect(result.current.live.liveTargetId).toBe(200);
  });

  it("无 LiveStream 时 liveTargetId 为 null", () => {
    const { result } = renderHook(() => useLiveConversation(7, 3));
    expect(result.current.live.liveTargetId).toBeNull();
  });

  it("doneTick 自增 → 调 reload 对账持久消息", () => {
    const { rerender } = renderHook(() => useLiveConversation(7, 3));
    reloadMock.mockClear();
    doneTick = 1;
    rerender();
    expect(reloadMock).toHaveBeenCalled();
  });
});
