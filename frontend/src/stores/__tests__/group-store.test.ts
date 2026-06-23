import { beforeEach, describe, expect, it } from "vitest";

import { useGroupStore, type GroupDetail } from "../group-store";

function detailWith(members: unknown[]): GroupDetail {
  return {
    group: { id: 5, title: "队", runStatus: "running", roundCount: 0 },
    members,
    messages: [],
  } as unknown as GroupDetail;
}

function detailWithTasks(tasks: unknown[]): GroupDetail {
  return {
    group: { id: 5, title: "队", runStatus: "running", roundCount: 0 },
    members: [],
    messages: [],
    tasks,
  } as unknown as GroupDetail;
}

describe("group-store patchMemberRunState", () => {
  beforeEach(() => {
    useGroupStore.setState({ details: new Map() });
  });

  it("updates only the target member's runState", () => {
    useGroupStore.getState().setDetail(
      5,
      detailWith([
        { id: 1, runState: "idle" },
        { id: 2, runState: "idle" },
      ]),
    );

    useGroupStore.getState().patchMemberRunState(5, 2, "running");

    const members = useGroupStore.getState().details.get(5)?.members;
    expect(members?.[0].runState).toBe("idle");
    expect(members?.[1].runState).toBe("running");
  });

  it("is a no-op when the member is absent", () => {
    useGroupStore
      .getState()
      .setDetail(5, detailWith([{ id: 1, runState: "running" }]));
    useGroupStore.getState().patchMemberRunState(5, 999, "idle");
    expect(useGroupStore.getState().details.get(5)?.members[0].runState).toBe(
      "running",
    );
  });
});

describe("group-store upsertTask", () => {
  beforeEach(() => {
    useGroupStore.setState({ details: new Map() });
  });

  it("Given 群详情已加载, When 收到新任务, Then 追加到 tasks 末尾", () => {
    useGroupStore.getState().setDetail(5, detailWithTasks([]));
    useGroupStore.getState().upsertTask(5, {
      id: 9,
      taskNo: 1,
      status: "open",
    } as never);
    const tasks = useGroupStore.getState().details.get(5)?.tasks;
    expect(tasks).toHaveLength(1);
    expect(tasks?.[0].id).toBe(9);
  });

  it("Given 任务已存在, When 收到同 id 更新, Then 原位替换(状态翻转)", () => {
    useGroupStore.getState().setDetail(
      5,
      detailWithTasks([
        { id: 9, taskNo: 1, status: "open" },
        { id: 10, taskNo: 2, status: "open" },
      ]),
    );
    useGroupStore.getState().upsertTask(5, {
      id: 9,
      taskNo: 1,
      status: "done",
      result: "改了 settings.tsx",
    } as never);
    const tasks = useGroupStore.getState().details.get(5)?.tasks;
    expect(tasks).toHaveLength(2);
    expect(tasks?.[0].status).toBe("done");
    expect(tasks?.[1].status).toBe("open");
  });

  it("Given 群详情未加载, When upsertTask, Then no-op 不崩", () => {
    useGroupStore.getState().upsertTask(404, { id: 9 } as never);
    expect(useGroupStore.getState().details.get(404)).toBeUndefined();
  });

  it("Given 缓存的旧详情没有 tasks 字段, When upsertTask, Then 当空数组起步", () => {
    useGroupStore.getState().setDetail(5, detailWith([]));
    useGroupStore.getState().upsertTask(5, { id: 9, taskNo: 1 } as never);
    expect(useGroupStore.getState().details.get(5)?.tasks).toHaveLength(1);
  });
});
