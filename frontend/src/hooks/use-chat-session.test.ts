import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { useChatSession } from "./use-chat-session";
import { useSessionMetaStore } from "@/stores/session-meta-store";
import { useSessionReadStore } from "@/stores/session-read-store";
import { useSessionStatusStore } from "@/stores/session-status-store";
import { useChatStreamsStore } from "@/stores/chat-streams-store";

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
    useChatStreamsStore.setState({ streams: new Map() });
  });

  // Bug: 群聊成员轮(及任何非前端发起的 turn)在中途打开会话时,前端没有 per-turn
  // 流入口 → 看不到"生成中"和流式内容。修复:LoadSession 在有活跃 turn 时回传
  // activeStream;hook 据此 openStream 重挂实时流。
  it("reattaches live stream on load when activeStream is present", async () => {
    loadChatSession.mockResolvedValueOnce({
      session: {
        id: 9,
        agentId: 1,
        agentName: "Eng",
        title: "x",
        agentStatus: "running",
        activeStream: "chat:event:9:42",
        lastMessageAt: 0,
        createtime: 0,
      },
      messages: [
        { id: 40, sessionId: 9, role: "user", blocks: [], seq: 1 },
        { id: 42, sessionId: 9, role: "assistant", blocks: [], seq: 2 },
      ],
    });
    const { result } = renderHook(() => useChatSession(9));
    await waitFor(() => expect(result.current.loading).toBe(false));

    const live = useChatStreamsStore.getState().streams.get(9);
    expect(live?.name).toBe("chat:event:9:42");
    expect(live?.assistantMessageId).toBe(42);
  });

  // Bug: 中途重开运行中的会话时,pending tool_approval 卡来自 LoadSession overlay
  // (后端把内存 pending 块 overlay 进末条 assistant 消息投影 → 渲染走 messages 路径)。
  // 用户点批准/拒绝后 resolved 事件只反扫 liveBlocks → no-op → 卡片永远 pending。
  // 修复:reattach 时把 pending tool_approval 块从消息副本剥离并搬进 liveBlocks
  // (单一真相源),resolved 事件自然命中,且不与消息路径双卡。
  it("moves overlay pending tool_approval blocks into liveBlocks on reattach", async () => {
    loadChatSession.mockResolvedValueOnce({
      session: {
        id: 9,
        agentId: 1,
        agentName: "Eng",
        title: "x",
        agentStatus: "running",
        activeStream: "chat:event:9:42",
        lastMessageAt: 0,
        createtime: 0,
      },
      messages: [
        { id: 40, sessionId: 9, role: "user", blocks: [], seq: 1 },
        {
          id: 42,
          sessionId: 9,
          role: "assistant",
          blocks: [
            { type: "text", text: "creating department..." },
            {
              type: "tool_approval",
              toolApproval: {
                requestId: "org-1",
                toolName: "org_create_department",
                toolInput: { name: "研发部" },
                status: "pending",
              },
            },
          ],
          seq: 2,
        },
      ],
    });
    const { result } = renderHook(() => useChatSession(9));
    await waitFor(() => expect(result.current.loading).toBe(false));

    // ① 消息路径不再含该 pending 块(防双卡);其余块保留。
    const lastMsg = result.current.messages.at(-1)!;
    expect((lastMsg.blocks ?? []).some((b) => b.type === "tool_approval")).toBe(
      false,
    );
    expect((lastMsg.blocks ?? []).some((b) => b.type === "text")).toBe(true);

    // ② live store 里出现该块,挂在重挂流上。
    const live = useChatStreamsStore.getState().streams.get(9);
    expect(live?.assistantMessageId).toBe(42);
    expect(live?.liveBlocks).toHaveLength(1);
    expect(live?.liveBlocks[0]).toMatchObject({
      type: "tool_approval",
      toolApproval: { requestId: "org-1", status: "pending" },
    });

    // ③ resolved 事件现在命中 liveBlocks,卡片翻 approved。
    act(() => {
      useChatStreamsStore.getState().markToolApprovalResolved(9, {
        toolKey: "org",
        requestId: "org-1",
        toolName: "org_create_department",
        status: "approved",
        result: "department created",
      });
    });
    const updated = useChatStreamsStore.getState().streams.get(9)!
      .liveBlocks[0];
    expect(updated.toolApproval).toMatchObject({
      status: "approved",
      result: "department created",
    });
  });

  // 已决议(resolved)的 tool_approval 块是持久化历史,留在 messages 路径,不搬不剥。
  it("leaves resolved tool_approval blocks in messages untouched on reattach", async () => {
    loadChatSession.mockResolvedValueOnce({
      session: {
        id: 9,
        agentId: 1,
        agentName: "Eng",
        title: "x",
        agentStatus: "running",
        activeStream: "chat:event:9:42",
        lastMessageAt: 0,
        createtime: 0,
      },
      messages: [
        {
          id: 42,
          sessionId: 9,
          role: "assistant",
          blocks: [
            {
              type: "tool_approval",
              toolApproval: {
                requestId: "org-0",
                toolName: "org_delete_agent",
                status: "approved",
                result: "done",
              },
            },
          ],
          seq: 1,
        },
      ],
    });
    const { result } = renderHook(() => useChatSession(9));
    await waitFor(() => expect(result.current.loading).toBe(false));

    const lastMsg = result.current.messages.at(-1)!;
    expect(
      (lastMsg.blocks ?? []).some(
        (b) =>
          b.type === "tool_approval" && b.toolApproval?.status === "approved",
      ),
    ).toBe(true);
    expect(useChatStreamsStore.getState().streams.get(9)?.liveBlocks).toEqual(
      [],
    );
  });

  // 同 tab 已有活跃流且 liveBlocks 已含同 requestId(流事件路径已写入)时,
  // mid-turn reload 返回的 overlay 块仍要从 messages 剥离,但不得重复注入。
  it("dedupes overlay pending tool_approval against an existing live block", async () => {
    act(() => {
      useChatStreamsStore.getState().openStream({
        name: "chat:event:9:42",
        sessionId: 9,
        assistantMessageId: 42,
        streamStartedAt: 123,
      });
      useChatStreamsStore.getState().appendLiveToolApproval(9, {
        toolKey: "org",
        requestId: "org-1",
        toolName: "org_create_department",
        status: "pending",
      });
    });
    loadChatSession.mockResolvedValueOnce({
      session: {
        id: 9,
        agentId: 1,
        agentName: "Eng",
        title: "x",
        agentStatus: "running",
        activeStream: "chat:event:9:42",
        lastMessageAt: 0,
        createtime: 0,
      },
      messages: [
        {
          id: 42,
          sessionId: 9,
          role: "assistant",
          blocks: [
            {
              type: "tool_approval",
              toolApproval: {
                requestId: "org-1",
                toolName: "org_create_department",
                status: "pending",
              },
            },
          ],
          seq: 1,
        },
      ],
    });
    const { result } = renderHook(() => useChatSession(9));
    await waitFor(() => expect(result.current.loading).toBe(false));

    const lastMsg = result.current.messages.at(-1)!;
    expect((lastMsg.blocks ?? []).some((b) => b.type === "tool_approval")).toBe(
      false,
    );
    const live = useChatStreamsStore.getState().streams.get(9);
    expect(live?.liveBlocks).toHaveLength(1);
  });

  it("does not reattach when activeStream is absent", async () => {
    loadChatSession.mockResolvedValueOnce({
      session: {
        id: 9,
        agentId: 1,
        agentName: "Eng",
        title: "x",
        agentStatus: "idle",
        lastMessageAt: 0,
        createtime: 0,
      },
      messages: [
        { id: 42, sessionId: 9, role: "assistant", blocks: [], seq: 1 },
      ],
    });
    const { result } = renderHook(() => useChatSession(9));
    await waitFor(() => expect(result.current.loading).toBe(false));

    expect(useChatStreamsStore.getState().streams.get(9)).toBeUndefined();
  });

  it("does not clobber an already-open live stream", async () => {
    // 模拟用户在自己会话里正常 Send 已经 openStream;reload 不得覆盖它。
    act(() => {
      useChatStreamsStore.getState().openStream({
        name: "chat:event:9:1",
        sessionId: 9,
        assistantMessageId: 1,
        streamStartedAt: 123,
      });
    });
    loadChatSession.mockResolvedValueOnce({
      session: {
        id: 9,
        agentId: 1,
        agentName: "Eng",
        title: "x",
        agentStatus: "running",
        activeStream: "chat:event:9:99",
        lastMessageAt: 0,
        createtime: 0,
      },
      messages: [
        { id: 99, sessionId: 9, role: "assistant", blocks: [], seq: 1 },
      ],
    });
    const { result } = renderHook(() => useChatSession(9));
    await waitFor(() => expect(result.current.loading).toBe(false));

    const live = useChatStreamsStore.getState().streams.get(9);
    expect(live?.name).toBe("chat:event:9:1");
    expect(live?.assistantMessageId).toBe(1);
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
