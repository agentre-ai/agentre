import { describe, it, expect, beforeEach } from "vitest";
import { useLocalCommandsStore } from "../local-commands-store";

describe("useLocalCommandsStore", () => {
  beforeEach(() => useLocalCommandsStore.setState({ entries: {} }));

  it("starts, appends, finishes", () => {
    const s = useLocalCommandsStore.getState();
    s.start({ id: "t1", sessionId: 5, command: "ls", createdAt: 100 });
    s.appendOutput("t1", "a");
    s.appendOutput("t1", "b");
    s.finish("t1", "done", 0);
    const e = useLocalCommandsStore.getState().get("t1")!;
    expect(e.output).toBe("ab");
    expect(e.status).toBe("done");
    expect(e.exitCode).toBe(0);
  });

  it("listForSession returns only that session, ordered by createdAt", () => {
    const s = useLocalCommandsStore.getState();
    s.start({ id: "b", sessionId: 5, command: "y", createdAt: 200 });
    s.start({ id: "a", sessionId: 5, command: "x", createdAt: 100 });
    s.start({ id: "c", sessionId: 9, command: "z", createdAt: 150 });
    const list = useLocalCommandsStore.getState().listForSession(5);
    expect(list.map((e) => e.id)).toEqual(["a", "b"]);
  });

  it("remove deletes only the targeted entry", () => {
    const s = useLocalCommandsStore.getState();
    s.start({ id: "r1", sessionId: 5, command: "x", createdAt: 100 });
    s.start({ id: "r2", sessionId: 5, command: "y", createdAt: 200 });
    s.remove("r1");
    expect(useLocalCommandsStore.getState().get("r1")).toBeUndefined();
    expect(useLocalCommandsStore.getState().get("r2")).toBeDefined();
  });

  it("remove on an unknown id is a no-op", () => {
    const s = useLocalCommandsStore.getState();
    s.start({ id: "r1", sessionId: 5, command: "x", createdAt: 100 });
    s.remove("missing");
    expect(useLocalCommandsStore.getState().listForSession(5)).toHaveLength(1);
  });
});
