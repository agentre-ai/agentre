import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const WorkflowCreate = vi.fn().mockResolvedValue({});
const WorkflowUpdate = vi.fn().mockResolvedValue({});
const WorkflowDelete = vi.fn().mockResolvedValue({});
const WorkflowList = vi.fn().mockResolvedValue({ items: [] });

vi.mock("../../../wailsjs/go/app/App", () => ({
  WorkflowCreate: (...a: unknown[]) => WorkflowCreate(...a),
  WorkflowUpdate: (...a: unknown[]) => WorkflowUpdate(...a),
  WorkflowDelete: (...a: unknown[]) => WorkflowDelete(...a),
  WorkflowList: (...a: unknown[]) => WorkflowList(...a),
}));

import { useWorkflows } from "../use-workflows";

describe("useWorkflows tags/outline", () => {
  beforeEach(() => vi.clearAllMocks());

  it("create 透传 tags/outline", async () => {
    const { result } = renderHook(() => useWorkflows());
    await waitFor(() => expect(WorkflowList).toHaveBeenCalled());
    await act(async () => {
      await result.current.create("n", "c", ["通用"], ["需求拆解", "方案设计"]);
    });
    expect(WorkflowCreate).toHaveBeenCalledWith({
      name: "n",
      content: "c",
      tags: ["通用"],
      outline: ["需求拆解", "方案设计"],
    });
  });

  it("update 透传 tags/outline", async () => {
    const { result } = renderHook(() => useWorkflows());
    await waitFor(() => expect(WorkflowList).toHaveBeenCalled());
    await act(async () => {
      await result.current.update(3, "n2", "c2", ["修复"], ["复现"]);
    });
    expect(WorkflowUpdate).toHaveBeenCalledWith({
      id: 3,
      name: "n2",
      content: "c2",
      tags: ["修复"],
      outline: ["复现"],
    });
  });

  it("reload 保留 tags/outline（无值时 fallback 空数组）", async () => {
    WorkflowList.mockResolvedValueOnce({
      items: [
        {
          id: 1,
          name: "w1",
          content: "c1",
          runCount: 0,
          createtime: 0,
          updatetime: 0,
          tags: ["tag1"],
          outline: ["step1"],
        },
        {
          id: 2,
          name: "w2",
          content: "c2",
          runCount: 0,
          createtime: 0,
          updatetime: 0,
          // tags/outline absent → should fallback to []
        },
      ],
    });
    const { result } = renderHook(() => useWorkflows());
    await waitFor(() => expect(result.current.workflows).toHaveLength(2));
    expect(result.current.workflows[0].tags).toEqual(["tag1"]);
    expect(result.current.workflows[0].outline).toEqual(["step1"]);
    expect(result.current.workflows[1].tags).toEqual([]);
    expect(result.current.workflows[1].outline).toEqual([]);
  });
});
