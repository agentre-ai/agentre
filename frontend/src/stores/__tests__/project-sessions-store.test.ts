import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../wailsjs/go/app/App", () => ({
  ProjectListTree: vi.fn(),
  ProjectListSessions: vi.fn(),
}));

import {
  ProjectListSessions,
  ProjectListTree,
} from "../../../wailsjs/go/app/App";
import {
  __resetProjectTreeForTesting,
  reloadProjectTreeCache,
} from "@/hooks/use-project-tree";

import { useProjectSessionsStore } from "../project-sessions-store";

const listTree = ProjectListTree as ReturnType<typeof vi.fn>;
const listSessions = ProjectListSessions as ReturnType<typeof vi.fn>;

describe("project-sessions-store", () => {
  beforeEach(() => {
    listTree.mockReset();
    listSessions.mockReset();
    __resetProjectTreeForTesting();
    useProjectSessionsStore.getState().__reset();
  });

  it("reload 拉所有 project 的 sessions 写入 sessionsByProject", async () => {
    listTree.mockResolvedValueOnce([
      {
        project: { id: 1, name: "A" },
        children: [{ project: { id: 2, name: "B" }, children: [] }],
      },
    ]);
    await reloadProjectTreeCache();
    listSessions.mockImplementation(async (pid: number) => {
      if (pid === 1) return [{ id: 10, title: "s10" }];
      if (pid === 2) return [{ id: 20, title: "s20" }];
      return [];
    });
    await useProjectSessionsStore.getState().reload();
    const state = useProjectSessionsStore.getState();
    expect(state.sessionsByProject.get(1)?.[0]?.id).toBe(10);
    expect(state.sessionsByProject.get(2)?.[0]?.id).toBe(20);
    expect(state.loading).toBe(false);
    expect(state.error).toBeNull();
  });

  it("tree 还没加载时, reload 不主动拉 tree, 也不发 sessions RPC", async () => {
    await useProjectSessionsStore.getState().reload();
    expect(listTree).not.toHaveBeenCalled();
    expect(listSessions).not.toHaveBeenCalled();
    expect(useProjectSessionsStore.getState().sessionsByProject.size).toBe(0);
    expect(useProjectSessionsStore.getState().loading).toBe(false);
  });

  it("tree 已加载但为空时, reload 不发 sessions RPC", async () => {
    listTree.mockResolvedValueOnce([]);
    await reloadProjectTreeCache();
    listTree.mockClear();
    await useProjectSessionsStore.getState().reload();
    expect(listTree).not.toHaveBeenCalled();
    expect(listSessions).not.toHaveBeenCalled();
    expect(useProjectSessionsStore.getState().sessionsByProject.size).toBe(0);
  });

  it("并发 reload dedupe: sessions RPC 只发一次", async () => {
    listTree.mockResolvedValueOnce([
      { project: { id: 1, name: "A" }, children: [] },
    ]);
    await reloadProjectTreeCache();

    listSessions.mockResolvedValue([]);
    const a = useProjectSessionsStore.getState().reload();
    const b = useProjectSessionsStore.getState().reload();
    await Promise.all([a, b]);
    // 两个 reload() 调用应复用同一 inflight, 因此 ProjectListSessions 只触发一次。
    expect(listSessions).toHaveBeenCalledTimes(1);
    expect(useProjectSessionsStore.getState().loading).toBe(false);
  });

  it("单个 project sessions RPC 失败 fallback 成 []", async () => {
    listTree.mockResolvedValueOnce([
      { project: { id: 1, name: "A" }, children: [] },
      { project: { id: 2, name: "B" }, children: [] },
    ]);
    await reloadProjectTreeCache();

    listSessions.mockImplementation(async (pid: number) => {
      if (pid === 1) throw new Error("oops");
      return [{ id: 20 }];
    });
    await useProjectSessionsStore.getState().reload();
    const state = useProjectSessionsStore.getState();
    expect(state.sessionsByProject.get(1)).toEqual([]);
    expect(state.sessionsByProject.get(2)).toEqual([{ id: 20 }]);
    expect(state.error).toBeNull();
  });

  it("再次 reload 拿到新快照", async () => {
    listTree.mockResolvedValueOnce([
      { project: { id: 1, name: "A" }, children: [] },
    ]);
    await reloadProjectTreeCache();

    listSessions.mockResolvedValueOnce([{ id: 10, title: "first" }]);
    await useProjectSessionsStore.getState().reload();
    expect(useProjectSessionsStore.getState().sessionsByProject.get(1)).toEqual(
      [{ id: 10, title: "first" }],
    );

    listSessions.mockResolvedValueOnce([
      { id: 10, title: "first" },
      { id: 11, title: "new" },
    ]);
    await useProjectSessionsStore.getState().reload();
    expect(
      useProjectSessionsStore.getState().sessionsByProject.get(1),
    ).toHaveLength(2);
  });
});
