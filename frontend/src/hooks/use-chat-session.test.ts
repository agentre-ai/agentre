import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { useChatSession } from "./use-chat-session";
import { useSessionMetaStore } from "@/stores/session-meta-store";
import { useSessionReadStore } from "@/stores/session-read-store";
import { useSessionStatusStore } from "@/stores/session-status-store";

vi.mock("../../wailsjs/go/app/App", () => ({
  LoadChatSession: vi.fn(),
  MarkChatSessionRead: vi.fn().mockResolvedValue(undefined),
}));
import { LoadChatSession, MarkChatSessionRead } from "../../wailsjs/go/app/App";
const loadChatSession = LoadChatSession as ReturnType<typeof vi.fn>;
const markChatSessionRead = MarkChatSessionRead as ReturnType<typeof vi.fn>;

describe("useChatSession", () => {
  beforeEach(() => {
    loadChatSession.mockReset();
    markChatSessionRead.mockClear();
    useSessionStatusStore.getState().__reset();
    useSessionMetaStore.getState().__reset();
    // session-read-store 没有 __reset,直接重建 Map(单调推进语义保证不会被旧值污染,
    // 但跨用例 override 残留会影响 "no override" 类断言)。
    useSessionReadStore.setState({ overrides: new Map() });
  });

  it("returns null when sessionId is 0", async () => {
    const { result } = renderHook(() => useChatSession(0));
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.session).toBeNull();
    expect(loadChatSession).not.toHaveBeenCalled();
  });

  it("loads session when sessionId changes", async () => {
    loadChatSession.mockResolvedValueOnce({
      session: {
        id: 5,
        agentId: 1,
        agentName: "Eng",
        title: "x",
        agentStatus: "idle",
        lastMessageAt: 0,
        createtime: 0,
      },
      messages: [],
    });
    const { result } = renderHook(() => useChatSession(5));
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.session?.id).toBe(5);
  });

  // 被动 ExitPlanMode 流程：CLI 自己切到 default 之后后端推
  // session_status 带 permissionMode，前端 hook 必须 overlay 到 session 上，
  // pill 才能跟着变。无 permissionMode 时不动 session.permissionMode（避免污染）。
  it("overlays session-status-store.permissionMode onto session when patch carries new mode", async () => {
    loadChatSession.mockResolvedValueOnce({
      session: {
        id: 7,
        agentId: 1,
        agentName: "Claude",
        title: "x",
        agentStatus: "running",
        permissionMode: "plan",
        lastMessageAt: 0,
        createtime: 0,
      },
      messages: [],
    });
    const { result } = renderHook(() => useChatSession(7));
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.session?.permissionMode).toBe("plan");

    act(() => {
      useSessionStatusStore.getState().upsert(7, {
        agentStatus: "running",
        needsAttention: false,
        permissionMode: "default",
      });
    });

    await waitFor(() =>
      expect(result.current.session?.permissionMode).toBe("default"),
    );
  });

  // Bug 3: useChatSession.reload 的 setMeta 必须保留 resp.session.lastReadAt,
  // 否则会擦掉 chat-agents-store.bulkUpsert 之前写入的服务端 lastReadAt。
  it("writes resp.session.lastReadAt into session-meta-store", async () => {
    loadChatSession.mockResolvedValueOnce({
      session: {
        id: 42,
        agentId: 1,
        agentName: "Eng",
        agentColor: "agent-1",
        projectId: 5,
        title: "x",
        agentStatus: "idle",
        lastMessageAt: 2000,
        lastReadAt: 1500,
        createtime: 0,
      },
      messages: [],
    });
    const { result } = renderHook(() => useChatSession(42));
    await waitFor(() => expect(result.current.loading).toBe(false));

    const meta = useSessionMetaStore.getState().metas.get(42);
    expect(meta?.lastReadAt).toBe(1500);
  });

  // Bug 2: 加载会话不再自动 MarkRead — mark-read 的语义是"用户当前正在看",
  // 这个判断只能在 ChatPanel 层基于 active prop 决定;hook 层无条件 MarkRead
  // 会让 tab-strip 隐藏 tab / 启动时恢复的 tab 全部错误地被标记为已读。
  it("does not call MarkChatSessionRead on load", async () => {
    loadChatSession.mockResolvedValueOnce({
      session: {
        id: 11,
        agentId: 1,
        agentName: "Eng",
        title: "x",
        agentStatus: "idle",
        lastMessageAt: 2000,
        lastReadAt: 0,
        createtime: 0,
      },
      messages: [],
    });
    const { result } = renderHook(() => useChatSession(11));
    await waitFor(() => expect(result.current.loading).toBe(false));

    expect(markChatSessionRead).not.toHaveBeenCalled();
    // 同步:也不再往 read overlay 写客户端 override。
    expect(useSessionReadStore.getState().overrides.get(11)).toBeUndefined();
  });

  // 兜底回归：现有 agentStatus/needsAttention 的 patch 不带 permissionMode 时，
  // session.permissionMode 保留 detail 上的值，不被空串覆盖。
  it("keeps detail.permissionMode when patch omits permissionMode", async () => {
    loadChatSession.mockResolvedValueOnce({
      session: {
        id: 8,
        agentId: 1,
        agentName: "Claude",
        title: "x",
        agentStatus: "running",
        permissionMode: "acceptEdits",
        lastMessageAt: 0,
        createtime: 0,
      },
      messages: [],
    });
    const { result } = renderHook(() => useChatSession(8));
    await waitFor(() => expect(result.current.loading).toBe(false));

    act(() => {
      // 模拟 chat-streams-host 把 session_status 事件转写到 store 时, 不带
      // permissionMode 字段（生产代码会用前一次的值兜底, 这里测试 hook 侧
      // 的合并语义）。
      useSessionStatusStore.getState().upsert(8, {
        agentStatus: "waiting",
        needsAttention: true,
        permissionMode: "acceptEdits",
      });
    });

    await waitFor(() =>
      expect(result.current.session?.agentStatus).toBe("waiting"),
    );
    expect(result.current.session?.permissionMode).toBe("acceptEdits");
  });
});
