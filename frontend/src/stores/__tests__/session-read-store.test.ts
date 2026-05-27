import { beforeEach, describe, expect, it } from "vitest";

import { useSessionReadStore, withReadOverlay } from "../session-read-store";

describe("useSessionReadStore", () => {
  beforeEach(() => {
    useSessionReadStore.setState({ overrides: new Map() });
  });

  it("markRead stores override and is single-source mutable across getState()", () => {
    useSessionReadStore.getState().markRead(42, 1700);
    expect(useSessionReadStore.getState().overrides.get(42)).toBe(1700);
  });

  it("markRead is monotonic — older ts ignored", () => {
    useSessionReadStore.getState().markRead(42, 2000);
    useSessionReadStore.getState().markRead(42, 1000);
    expect(useSessionReadStore.getState().overrides.get(42)).toBe(2000);
  });

  it("markRead ignores invalid sessionId/ts", () => {
    useSessionReadStore.getState().markRead(0, 1700);
    useSessionReadStore.getState().markRead(42, 0);
    useSessionReadStore.getState().markRead(-1, 1700);
    expect(useSessionReadStore.getState().overrides.size).toBe(0);
  });

  it("markRead is reference-stable when value didn't change (no extra re-renders)", () => {
    useSessionReadStore.getState().markRead(42, 1700);
    const before = useSessionReadStore.getState().overrides;
    useSessionReadStore.getState().markRead(42, 500);
    expect(useSessionReadStore.getState().overrides).toBe(before);
  });
});

describe("withReadOverlay", () => {
  it("returns same object when no override", () => {
    const s = { id: 1, lastReadAt: 100 };
    expect(withReadOverlay(s, new Map())).toBe(s);
  });

  it("returns same object when override <= server value", () => {
    const s = { id: 1, lastReadAt: 500 };
    expect(withReadOverlay(s, new Map([[1, 300]]))).toBe(s);
  });

  it("returns new object with override when override > server value", () => {
    const s = { id: 1, lastReadAt: 100 };
    const out = withReadOverlay(s, new Map([[1, 500]]));
    expect(out).not.toBe(s);
    expect(out.lastReadAt).toBe(500);
  });

  it("treats missing lastReadAt as 0 (overrides any positive)", () => {
    const s = { id: 1 } as { id: number; lastReadAt?: number };
    const out = withReadOverlay(s, new Map([[1, 500]]));
    expect(out.lastReadAt).toBe(500);
  });

  it("preserves extra fields on overlay", () => {
    const s = { id: 1, lastReadAt: 100, title: "hello", agentStatus: "idle" };
    const out = withReadOverlay(s, new Map([[1, 500]]));
    expect(out).toEqual({
      id: 1,
      lastReadAt: 500,
      title: "hello",
      agentStatus: "idle",
    });
  });
});
