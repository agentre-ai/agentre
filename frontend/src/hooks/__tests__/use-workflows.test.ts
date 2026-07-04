import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const appMocks = vi.hoisted(() => ({
  WorkflowList: vi.fn(),
  WorkflowCreate: vi.fn(),
  WorkflowUpdate: vi.fn(),
  WorkflowDelete: vi.fn(),
}));
vi.mock("../../../wailsjs/go/app/App", () => appMocks);

import { useWorkflows } from "../use-workflows";

describe("useWorkflows graph 打通", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    appMocks.WorkflowList.mockResolvedValue({ items: [] });
    appMocks.WorkflowCreate.mockResolvedValue({});
    appMocks.WorkflowUpdate.mockResolvedValue({});
  });

  it("投影出的 WorkflowItem 携带 graph 字段", async () => {
    appMocks.WorkflowList.mockResolvedValue({
      items: [
        {
          id: 1,
          name: "F",
          content: "c",
          tags: [],
          outline: [],
          runCount: 0,
          createtime: 0,
          updatetime: 0,
          graph: '{"version":1,"nodes":[{"id":"n1","label":"x","kind":"task"}],"edges":[]}',
        },
      ],
    });
    const { result } = renderHook(() => useWorkflows());
    await waitFor(() => expect(result.current.workflows.length).toBe(1));
    expect(result.current.workflows[0].graph).toContain('"n1"');
  });

  it("create 把 graph 传给 WorkflowCreate", async () => {
    const { result } = renderHook(() => useWorkflows());
    await waitFor(() => expect(appMocks.WorkflowList).toHaveBeenCalled());
    await act(async () => {
      await result.current.create("N", "C", [], [], '{"g":1}');
    });
    expect(appMocks.WorkflowCreate).toHaveBeenCalledWith(
      expect.objectContaining({ name: "N", graph: '{"g":1}' }),
    );
  });

  it("update 把 graph 传给 WorkflowUpdate; 省略时为空串", async () => {
    const { result } = renderHook(() => useWorkflows());
    await waitFor(() => expect(appMocks.WorkflowList).toHaveBeenCalled());
    await act(async () => {
      await result.current.update(7, "N", "C", [], []);
    });
    expect(appMocks.WorkflowUpdate).toHaveBeenCalledWith(
      expect.objectContaining({ id: 7, graph: "" }),
    );
  });
});
