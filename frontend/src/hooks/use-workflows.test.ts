import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const workflowList = vi.fn();
const workflowCreate = vi.fn();
const workflowUpdate = vi.fn();
const workflowDelete = vi.fn();

vi.mock("../../wailsjs/go/app/App", () => ({
  WorkflowList: (...a: unknown[]) => workflowList(...a),
  WorkflowCreate: (...a: unknown[]) => workflowCreate(...a),
  WorkflowUpdate: (...a: unknown[]) => workflowUpdate(...a),
  WorkflowDelete: (...a: unknown[]) => workflowDelete(...a),
}));

import { useWorkflows } from "./use-workflows";

describe("useWorkflows", () => {
  beforeEach(() => {
    workflowList.mockReset().mockResolvedValue({
      items: [
        {
          id: 1,
          name: "产品开发流程",
          content: "# A",
          groupCount: 2,
          createtime: 1,
          updatetime: 2,
        },
      ],
    });
    workflowCreate.mockReset().mockResolvedValue({ item: { id: 9 } });
    workflowUpdate.mockReset().mockResolvedValue({ item: { id: 1 } });
    workflowDelete.mockReset().mockResolvedValue({});
  });

  it("挂载即加载列表", async () => {
    const { result } = renderHook(() => useWorkflows());
    await waitFor(() => expect(result.current.workflows).toHaveLength(1));
    expect(result.current.workflows[0].name).toBe("产品开发流程");
    expect(result.current.workflows[0].groupCount).toBe(2);
  });

  it("create/update/remove 调绑定后重新加载", async () => {
    const { result } = renderHook(() => useWorkflows());
    await waitFor(() => expect(result.current.workflows).toHaveLength(1));
    await act(async () => {
      await result.current.create("新流程", "# 新");
    });
    expect(workflowCreate).toHaveBeenCalledWith({
      name: "新流程",
      content: "# 新",
    });
    await act(async () => {
      await result.current.update(1, "改名", "# 改");
    });
    expect(workflowUpdate).toHaveBeenCalledWith({
      id: 1,
      name: "改名",
      content: "# 改",
    });
    await act(async () => {
      await result.current.remove(1);
    });
    expect(workflowDelete).toHaveBeenCalledWith({ id: 1 });
    // 初始 1 次 + 三个写操作后各 reload 1 次
    expect(workflowList).toHaveBeenCalledTimes(4);
  });

  it("加载失败落 error", async () => {
    workflowList.mockRejectedValueOnce(new Error("boom"));
    const { result } = renderHook(() => useWorkflows());
    await waitFor(() => expect(result.current.error).toBe("boom"));
    expect(result.current.workflows).toHaveLength(0);
  });

  it("remove 失败落 error 且不重载", async () => {
    const { result } = renderHook(() => useWorkflows());
    await waitFor(() => expect(result.current.workflows).toHaveLength(1));
    workflowDelete.mockRejectedValueOnce(new Error("nope"));
    await act(async () => {
      await result.current.remove(1);
    });
    expect(result.current.error).toBe("nope");
    expect(workflowList).toHaveBeenCalledTimes(1);
  });
});
