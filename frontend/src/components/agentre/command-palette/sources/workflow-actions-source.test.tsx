import { beforeEach, describe, expect, it, vi } from "vitest";

import { useWorkflowManagerStore } from "@/stores/workflow-manager-store";

import { workflowActionsSource } from "./workflow-actions-source";
import type { OnSelectCtx } from "../types";

function ctx(): OnSelectCtx {
  return {
    navigate: vi.fn() as unknown as OnSelectCtx["navigate"],
    close: vi.fn(),
    openSession: vi.fn(),
    openNewSession: vi.fn(),
    pathname: "/chat",
  };
}

describe("workflowActionsSource", () => {
  beforeEach(() => {
    useWorkflowManagerStore.setState({ open: false, intent: "browse" });
  });

  it("命令模式下两条命令(open / new)", () => {
    expect(workflowActionsSource.modes).toContain("command");
    const { items } = workflowActionsSource.useItems();
    expect(items.map((i) => i.key)).toEqual([
      "workflow-open-library",
      "workflow-new",
    ]);
  });

  it("open 命令:close + openBrowse", () => {
    const c = ctx();
    const open = workflowActionsSource
      .useItems()
      .items.find((i) => i.key === "workflow-open-library")!;
    workflowActionsSource.onSelect(open, c);
    expect(c.close).toHaveBeenCalled();
    expect(useWorkflowManagerStore.getState().open).toBe(true);
    expect(useWorkflowManagerStore.getState().intent).toBe("browse");
  });

  it("new 命令:close + openCreate", () => {
    const c = ctx();
    const item = workflowActionsSource
      .useItems()
      .items.find((i) => i.key === "workflow-new")!;
    workflowActionsSource.onSelect(item, c);
    expect(c.close).toHaveBeenCalled();
    expect(useWorkflowManagerStore.getState().intent).toBe("create");
  });

  it("getScore:标题命中 query 返回正分,不命中 0", () => {
    const item = workflowActionsSource
      .useItems()
      .items.find((i) => i.key === "workflow-open-library")!;
    expect(workflowActionsSource.getScore("workflow", item)).toBeGreaterThan(0);
    expect(workflowActionsSource.getScore("zzzzz", item)).toBe(0);
  });
});
