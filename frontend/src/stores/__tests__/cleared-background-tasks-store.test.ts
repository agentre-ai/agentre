import { beforeEach, describe, expect, it } from "vitest";

import { useClearedBackgroundTasksStore } from "../cleared-background-tasks-store";

describe("cleared-background-tasks-store", () => {
  beforeEach(() => {
    useClearedBackgroundTasksStore.setState({ cleared: {} });
    localStorage.clear();
  });

  it("records cleared toolUseIds per session", () => {
    useClearedBackgroundTasksStore.getState().clearCompleted(7, ["a", "b"]);
    expect(useClearedBackgroundTasksStore.getState().cleared[7]).toEqual([
      "a",
      "b",
    ]);
  });

  it("merges + dedupes on repeated clears", () => {
    const { clearCompleted } = useClearedBackgroundTasksStore.getState();
    clearCompleted(7, ["a"]);
    clearCompleted(7, ["a", "c"]);
    expect(useClearedBackgroundTasksStore.getState().cleared[7]).toEqual([
      "a",
      "c",
    ]);
  });

  it("keeps sessions independent", () => {
    const { clearCompleted } = useClearedBackgroundTasksStore.getState();
    clearCompleted(7, ["a"]);
    clearCompleted(8, ["b"]);
    expect(useClearedBackgroundTasksStore.getState().cleared[7]).toEqual(["a"]);
    expect(useClearedBackgroundTasksStore.getState().cleared[8]).toEqual(["b"]);
  });

  it("ignores empty clears", () => {
    useClearedBackgroundTasksStore.getState().clearCompleted(7, []);
    expect(
      useClearedBackgroundTasksStore.getState().cleared[7],
    ).toBeUndefined();
  });
});
