import { renderHook, act } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

const sendMock = vi.fn();
const enqueueMock = vi.fn();
const openStreamMock = vi.fn();
const markRunningMock = vi.fn();

vi.mock("../../wailsjs/go/app/App", () => ({
  SendChatMessage: (...a: unknown[]) => sendMock(...a),
  EnqueueChatMessage: (...a: unknown[]) => enqueueMock(...a),
}));
vi.mock("../../wailsjs/go/models", () => ({
  chat_svc: { SendRequest: { createFrom: (x: unknown) => x } },
}));
vi.mock("@/stores/chat-streams-store", () => ({
  useChatStreamsStore: Object.assign(
    (sel: (s: unknown) => unknown) => sel({ openStream: openStreamMock }),
    { getState: () => ({ openStream: openStreamMock }) },
  ),
}));
vi.mock("@/stores/session-status-store", () => ({
  markSessionRunning: (...a: unknown[]) => markRunningMock(...a),
}));
vi.mock("@/components/agentre/capability/use-backend-capabilities", () => ({
  useBackendCapabilities: () => ({
    caps: new Set(["set_permission_mode", "image_input"]),
  }),
}));
const sessionCapsMock = vi.fn(
  (..._a: unknown[]): { caps: Set<string> | null } => ({ caps: null }),
);
vi.mock("@/components/agentre/capability/use-session-capabilities", () => ({
  useSessionCapabilities: (...a: unknown[]) => sessionCapsMock(...a),
}));
vi.mock("@/components/agentre/permission-mode", () => ({
  usePermissionMode: () => ({ mode: "default", setMode: vi.fn() }),
  PermissionModePill: () => null,
}));

import { useComposerSend } from "./use-composer-send";

describe("useComposerSend", () => {
  beforeEach(() => {
    sendMock.mockReset();
    enqueueMock.mockReset();
    openStreamMock.mockReset();
    markRunningMock.mockReset();
    sessionCapsMock.mockReset();
    sessionCapsMock.mockReturnValue({ caps: null });
  });

  it("idle 时 submit 走 SendChatMessage + openStream + optimistic", async () => {
    sendMock.mockResolvedValue({
      sessionId: 7,
      userMessageId: 100,
      assistantMessageId: 101,
      stream: "chat:stream:7:101",
    });
    const onOptimistic = vi.fn();
    const { result } = renderHook(() =>
      useComposerSend({
        sessionId: 7,
        agentId: 3,
        backendType: "claudecode",
        isRunning: false,
        onOptimistic,
      }),
    );
    await act(async () => {
      await result.current.submit({ text: "hi" });
    });
    expect(sendMock).toHaveBeenCalledWith(
      expect.objectContaining({
        sessionId: 7,
        agentId: 3,
        text: "hi",
        permissionMode: "default",
      }),
    );
    expect(openStreamMock).toHaveBeenCalledWith(
      expect.objectContaining({
        name: "chat:stream:7:101",
        sessionId: 7,
        assistantMessageId: 101,
      }),
    );
    expect(markRunningMock).toHaveBeenCalledWith(7);
    expect(onOptimistic).toHaveBeenCalled();
    expect(enqueueMock).not.toHaveBeenCalled();
  });

  it("isRunning 时 submit 走 EnqueueChatMessage,不新起 turn", async () => {
    enqueueMock.mockResolvedValue({ queuedId: "q1" });
    const { result } = renderHook(() =>
      useComposerSend({
        sessionId: 7,
        agentId: 3,
        backendType: "claudecode",
        isRunning: true,
      }),
    );
    await act(async () => {
      await result.current.submit({ text: "later" });
    });
    expect(enqueueMock).toHaveBeenCalledWith({ sessionId: 7, text: "later" });
    expect(sendMock).not.toHaveBeenCalled();
  });

  it("空文本 submit 直接忽略", async () => {
    const { result } = renderHook(() =>
      useComposerSend({
        sessionId: 7,
        agentId: 3,
        backendType: "claudecode",
        isRunning: false,
      }),
    );
    await act(async () => {
      await result.current.submit({ text: "   " });
    });
    expect(sendMock).not.toHaveBeenCalled();
    expect(enqueueMock).not.toHaveBeenCalled();
  });

  it("已有会话(sessionId>0)时 caps 取自 useSessionCapabilities 而非 useBackendCapabilities", () => {
    // 会话能力矩阵里没有 set_permission_mode/image_input(即便 backend caps mock 里有)
    sessionCapsMock.mockReturnValue({
      caps: new Set<string>(),
    });
    const { result: withoutModeResult } = renderHook(() =>
      useComposerSend({
        sessionId: 7,
        agentId: 3,
        backendType: "claudecode",
        isRunning: false,
      }),
    );
    expect(withoutModeResult.current.isModeSwitchable).toBe(false);
    expect(withoutModeResult.current.supportsImageInput).toBe(false);

    sessionCapsMock.mockReturnValue({
      caps: new Set(["set_permission_mode", "image_input"]),
    });
    const { result: withModeResult } = renderHook(() =>
      useComposerSend({
        sessionId: 7,
        agentId: 3,
        backendType: "claudecode",
        isRunning: false,
      }),
    );
    expect(withModeResult.current.isModeSwitchable).toBe(true);
    expect(withModeResult.current.supportsImageInput).toBe(true);
  });
});
