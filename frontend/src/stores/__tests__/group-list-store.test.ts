import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../../wailsjs/go/app/App", () => ({
  GroupList: vi.fn(),
}));

import { useGroupListStore, type GroupListItem } from "../group-list-store";

function seedGroups(): void {
  useGroupListStore.setState({
    groups: [
      { id: 5, title: "队", runStatus: "waiting_user", pinned: false },
      { id: 6, title: "另一队", runStatus: "idle", pinned: false },
    ] as unknown as GroupListItem[],
    loading: false,
    error: null,
  });
}

// 回归(侧栏 running 不亮): 群列表 store 原先只能整体 reload, 后端 run_status
// 事件无处可落 —— 侧栏群行状态点永远停在上次 reload 的值。
describe("group-list-store patchRunStatus", () => {
  beforeEach(() => {
    useGroupListStore.getState().__reset();
    seedGroups();
  });

  it("updates only the target group's runStatus", () => {
    useGroupListStore.getState().patchRunStatus(5, "running");
    const groups = useGroupListStore.getState().groups;
    expect(groups[0].runStatus).toBe("running");
    expect(groups[1].runStatus).toBe("idle");
  });

  it("is a no-op for an unknown group id", () => {
    const before = useGroupListStore.getState().groups;
    useGroupListStore.getState().patchRunStatus(999, "running");
    expect(useGroupListStore.getState().groups).toBe(before);
  });

  it("short-circuits when the value is unchanged", () => {
    const before = useGroupListStore.getState().groups;
    useGroupListStore.getState().patchRunStatus(5, "waiting_user");
    expect(useGroupListStore.getState().groups).toBe(before);
  });
});
