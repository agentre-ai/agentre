import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../wailsjs/go/app/App", () => ({
  ProjectListTree: vi.fn(),
}));

import { ProjectListTree } from "../../../wailsjs/go/app/App";
import { useProjectList } from "../use-project-list";

const projectListTree = ProjectListTree as ReturnType<typeof vi.fn>;

describe("useProjectList", () => {
  beforeEach(() => {
    projectListTree.mockReset();
  });

  it("loads projects on mount", async () => {
    projectListTree.mockResolvedValueOnce([
      { project: { id: 1, name: "Agentre", path: "/a", color: "agent-1" } },
    ]);
    const { result } = renderHook(() => useProjectList());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.projects).toEqual([
      { id: 1, name: "Agentre", path: "/a", color: "agent-1" },
    ]);
    expect(result.current.error).toBeNull();
  });

  it("captures error", async () => {
    projectListTree.mockRejectedValueOnce(new Error("boom"));
    const { result } = renderHook(() => useProjectList());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.error).toBe("boom");
  });

  it("reload re-fetches", async () => {
    projectListTree.mockResolvedValueOnce([]);
    const { result } = renderHook(() => useProjectList());
    await waitFor(() => expect(result.current.loading).toBe(false));

    projectListTree.mockResolvedValueOnce([
      { project: { id: 2, name: "X", path: "/x", color: "agent-2" } },
    ]);
    await result.current.reload();
    await waitFor(() => expect(result.current.projects).toHaveLength(1));
  });

  // 决定性断言: 3 个 hook 实例并发 mount (模拟 3 个同时打开的 chat tab 各自
  // 渲染 ChatComposer → useProjectList) 只应触发一次 ProjectListTree IPC
  // 往返, 且所有实例最终拿到同一份数据。修复前每个 hook 实例各自
  // useState+useEffect, 这里会失败为 3 次调用。
  it("并发 3 个 hook 实例只触发一次 ProjectListTree, 且都拿到同一份数据", async () => {
    projectListTree.mockResolvedValue([
      { project: { id: 1, name: "Agentre", path: "/a", color: "agent-1" } },
    ]);
    const { result: a } = renderHook(() => useProjectList());
    const { result: b } = renderHook(() => useProjectList());
    const { result: c } = renderHook(() => useProjectList());

    await waitFor(() => expect(a.current.loading).toBe(false));
    await waitFor(() => expect(b.current.loading).toBe(false));
    await waitFor(() => expect(c.current.loading).toBe(false));

    expect(projectListTree).toHaveBeenCalledTimes(1);
    expect(a.current.projects).toEqual(b.current.projects);
    expect(a.current.projects).toEqual(c.current.projects);
    expect(a.current.projects).toEqual([
      { id: 1, name: "Agentre", path: "/a", color: "agent-1" },
    ]);
  });
});
