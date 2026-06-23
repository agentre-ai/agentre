import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../wailsjs/go/app/App", () => ({
  GroupLoad: vi.fn(),
  GroupSend: vi.fn(),
}));
vi.mock("../../wailsjs/runtime/runtime", () => ({
  EventsOn: vi.fn(() => () => {}),
  EventsOff: vi.fn(),
}));

import { GroupLoad } from "../../wailsjs/go/app/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { useGroupStore } from "../stores/group-store";
import { useGroup } from "./use-group";

describe("useGroup", () => {
  beforeEach(() => {
    // zustand store 是模块单例，state 跨测试持久 —— 每个 case 前清空，
    // 避免 test 1 写入的 detail 漏给 test 2（de-dupe 让追加判断走偏）。
    useGroupStore.setState({ details: new Map() });
    (GroupLoad as ReturnType<typeof vi.fn>).mockResolvedValue({
      group: { id: 5, title: "队", runStatus: "running", roundCount: 3 },
      members: [
        {
          id: 1,
          agentID: 2,
          role: "host",
          status: "active",
          backingSessionID: 11,
        },
      ],
      messages: [
        {
          id: 1,
          seq: 1,
          senderKind: "user",
          content: "hi",
          recipientMemberIDs: [1],
          toUser: false,
        },
      ],
      tasks: [],
    });
  });

  it("loads group detail on mount and subscribes to events", async () => {
    const { result } = renderHook(() => useGroup(5));
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.detail?.group?.title).toBe("队");
    expect(result.current.detail?.members).toHaveLength(1);
    expect(EventsOn).toHaveBeenCalledWith(
      "group:event:5",
      expect.any(Function),
    );
  });

  it("appends a live message event into the store", async () => {
    let handler: ((p: unknown) => void) | undefined;
    (EventsOn as ReturnType<typeof vi.fn>).mockImplementation(
      (_e: string, h: (p: unknown) => void) => {
        handler = h;
        return () => {};
      },
    );
    const { result } = renderHook(() => useGroup(5));
    await waitFor(() => expect(result.current.loading).toBe(false));
    handler?.({
      kind: "message",
      message: {
        id: 2,
        seq: 2,
        senderKind: "agent",
        content: "done",
        recipientMemberIDs: [],
        toUser: true,
      },
    });
    await waitFor(() =>
      expect(result.current.detail?.messages).toHaveLength(2),
    );
    expect(result.current.detail?.messages[1].content).toBe("done");
  });

  it("patches a member's runState on a member_run_state event", async () => {
    let handler: ((p: unknown) => void) | undefined;
    (EventsOn as ReturnType<typeof vi.fn>).mockImplementation(
      (_e: string, h: (p: unknown) => void) => {
        handler = h;
        return () => {};
      },
    );
    const { result } = renderHook(() => useGroup(5));
    await waitFor(() => expect(result.current.loading).toBe(false));
    handler?.({ kind: "member_run_state", memberID: 1, runState: "running" });
    await waitFor(() =>
      expect(result.current.detail?.members[0].runState).toBe("running"),
    );
  });

  it("patches a member when the backend lazy-creates its backing session", async () => {
    let handler: ((p: unknown) => void) | undefined;
    (EventsOn as ReturnType<typeof vi.fn>).mockImplementation(
      (_e: string, h: (p: unknown) => void) => {
        handler = h;
        return () => {};
      },
    );
    const { result } = renderHook(() => useGroup(5));
    await waitFor(() => expect(result.current.loading).toBe(false));
    handler?.({
      kind: "member_updated",
      member: {
        id: 1,
        agentID: 2,
        role: "host",
        status: "active",
        backingSessionID: 77,
      },
    });
    await waitFor(() =>
      expect(result.current.detail?.members[0].backingSessionID).toBe(77),
    );
  });

  it("upserts a task on a task_updated event (创建 + 状态翻转)", async () => {
    let handler: ((p: unknown) => void) | undefined;
    (EventsOn as ReturnType<typeof vi.fn>).mockImplementation(
      (_e: string, h: (p: unknown) => void) => {
        handler = h;
        return () => {};
      },
    );
    const { result } = renderHook(() => useGroup(5));
    await waitFor(() => expect(result.current.loading).toBe(false));

    handler?.({
      kind: "task_updated",
      task: {
        id: 9,
        taskNo: 1,
        title: "重构设置页",
        brief: "按设计稿",
        creatorMemberID: 1,
        assigneeMemberID: 2,
        status: "open",
        result: "",
        parentTaskNo: 0,
      },
    });
    await waitFor(() => expect(result.current.detail?.tasks).toHaveLength(1));

    handler?.({
      kind: "task_updated",
      task: {
        id: 9,
        taskNo: 1,
        title: "重构设置页",
        brief: "按设计稿",
        creatorMemberID: 1,
        assigneeMemberID: 2,
        status: "done",
        result: "改完自测通过",
        parentTaskNo: 0,
      },
    });
    await waitFor(() =>
      expect(result.current.detail?.tasks?.[0].status).toBe("done"),
    );
  });
});
