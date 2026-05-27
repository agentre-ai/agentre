import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  invalidateProjectTreeCache,
  useProjectTree,
  __resetProjectTreeForTesting,
} from "../use-project-tree";

vi.mock("../../../wailsjs/go/app/App", () => ({
  ProjectListTree: vi.fn(),
}));

import { ProjectListTree } from "../../../wailsjs/go/app/App";

beforeEach(() => {
  __resetProjectTreeForTesting();
  (ProjectListTree as ReturnType<typeof vi.fn>).mockReset();
});

describe("useProjectTree", () => {
  it("首次 mount 拉取 tree, 缓存后多个 hook 共享同一份", async () => {
    (ProjectListTree as ReturnType<typeof vi.fn>).mockResolvedValue([
      { project: { id: 1, name: "Agentre" }, children: [] },
    ]);
    const { result: r1 } = renderHook(() => useProjectTree());
    await waitFor(() => {
      expect(r1.current.tree).toHaveLength(1);
    });
    const { result: r2 } = renderHook(() => useProjectTree());
    expect(r2.current.tree).toBe(r1.current.tree);
    expect(ProjectListTree).toHaveBeenCalledTimes(1);
  });

  it("invalidate() 重拉", async () => {
    (ProjectListTree as ReturnType<typeof vi.fn>).mockResolvedValue([]);
    const { result } = renderHook(() => useProjectTree());
    await waitFor(() => expect(result.current.tree).toEqual([]));
    result.current.invalidate();
    await waitFor(() => expect(ProjectListTree).toHaveBeenCalledTimes(2));
  });

  it("外部 invalidateProjectTreeCache() 会更新已挂载的 hook", async () => {
    (ProjectListTree as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce([
        { project: { id: 1, name: "Agentre", color: "agent-1" }, children: [] },
      ])
      .mockResolvedValueOnce([
        { project: { id: 1, name: "Agentre", color: "agent-2" }, children: [] },
      ]);

    const { result } = renderHook(() => useProjectTree());
    await waitFor(() => {
      expect(result.current.tree[0]?.project?.color).toBe("agent-1");
    });

    invalidateProjectTreeCache();

    await waitFor(() => {
      expect(result.current.tree[0]?.project?.color).toBe("agent-2");
    });
    expect(ProjectListTree).toHaveBeenCalledTimes(2);
  });
});
