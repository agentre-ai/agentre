import { beforeEach, describe, expect, it } from "vitest";

import { useQueuedMessagesStore } from "../queued-messages-store";

describe("queued-messages-store", () => {
  beforeEach(() => useQueuedMessagesStore.getState().__reset());

  it("append / consume all / clear", () => {
    useQueuedMessagesStore
      .getState()
      .append(1, { id: "a", text: "hello", cancellable: true });
    useQueuedMessagesStore
      .getState()
      .append(1, { id: "b", text: "world", cancellable: false });
    expect(
      useQueuedMessagesStore.getState().queuedBySession.get(1)?.length,
    ).toBe(2);

    const consumed = useQueuedMessagesStore.getState().consume(1);
    expect(consumed.map((m) => m.id)).toEqual(["a", "b"]);
    expect(useQueuedMessagesStore.getState().queuedBySession.has(1)).toBe(
      false,
    );
  });

  it("consume with ids removes matching entries", () => {
    useQueuedMessagesStore
      .getState()
      .append(1, { id: "a", text: "1", cancellable: true });
    useQueuedMessagesStore
      .getState()
      .append(1, { id: "b", text: "2", cancellable: true });
    useQueuedMessagesStore
      .getState()
      .append(1, { id: "c", text: "3", cancellable: false });

    const removed = useQueuedMessagesStore.getState().consume(1, ["a", "c"]);
    expect(removed.map((m) => m.id)).toEqual(["a", "c"]);
    const remaining = useQueuedMessagesStore.getState().queuedBySession.get(1);
    expect(remaining?.map((m) => m.id)).toEqual(["b"]);
  });

  it("clear removes all entries for session", () => {
    useQueuedMessagesStore
      .getState()
      .append(2, { id: "x", text: "x", cancellable: true });
    useQueuedMessagesStore.getState().clear(2);
    expect(useQueuedMessagesStore.getState().queuedBySession.has(2)).toBe(
      false,
    );
  });

  it("per-session isolation", () => {
    useQueuedMessagesStore
      .getState()
      .append(1, { id: "a", text: "1", cancellable: true });
    useQueuedMessagesStore
      .getState()
      .append(2, { id: "b", text: "2", cancellable: true });
    useQueuedMessagesStore.getState().clear(1);
    expect(useQueuedMessagesStore.getState().queuedBySession.has(1)).toBe(
      false,
    );
    expect(
      useQueuedMessagesStore.getState().queuedBySession.get(2)?.length,
    ).toBe(1);
  });

  it("clear on non-existent session is a no-op (referential stability)", () => {
    useQueuedMessagesStore
      .getState()
      .append(1, { id: "a", text: "1", cancellable: true });
    const before = useQueuedMessagesStore.getState().queuedBySession;
    useQueuedMessagesStore.getState().clear(999);
    expect(useQueuedMessagesStore.getState().queuedBySession).toBe(before);
  });
});
