import { beforeEach, describe, expect, it } from "vitest";

import { useWorkflowManagerStore } from "./workflow-manager-store";

describe("useWorkflowManagerStore", () => {
  beforeEach(() => {
    useWorkflowManagerStore.setState({ open: false, intent: "browse" });
  });

  it("openBrowse 打开且 intent=browse", () => {
    useWorkflowManagerStore.getState().openBrowse();
    expect(useWorkflowManagerStore.getState().open).toBe(true);
    expect(useWorkflowManagerStore.getState().intent).toBe("browse");
  });

  it("openCreate 打开且 intent=create", () => {
    useWorkflowManagerStore.getState().openCreate();
    expect(useWorkflowManagerStore.getState().open).toBe(true);
    expect(useWorkflowManagerStore.getState().intent).toBe("create");
  });

  it("close 关闭并把 intent 复位为 browse", () => {
    useWorkflowManagerStore.getState().openCreate();
    useWorkflowManagerStore.getState().close();
    expect(useWorkflowManagerStore.getState().open).toBe(false);
    expect(useWorkflowManagerStore.getState().intent).toBe("browse");
  });
});
