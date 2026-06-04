import { beforeEach, describe, expect, it } from "vitest";

import { useNotificationToastStore } from "../notification-toast-store";

beforeEach(() => useNotificationToastStore.getState().clear());

describe("notification-toast-store", () => {
  it("push 追加一条并返回自增 id", () => {
    const a = useNotificationToastStore
      .getState()
      .push({ sessionId: 1, kind: "done", title: "A", body: "完成" });
    const b = useNotificationToastStore
      .getState()
      .push({ sessionId: 2, kind: "error", title: "B", body: "出错" });
    expect(b).toBeGreaterThan(a);
    const { toasts } = useNotificationToastStore.getState();
    expect(toasts).toHaveLength(2);
    expect(toasts[0]).toMatchObject({
      id: a,
      sessionId: 1,
      kind: "done",
      title: "A",
      body: "完成",
    });
  });

  it("dismiss 按 id 移除", () => {
    const a = useNotificationToastStore
      .getState()
      .push({ sessionId: 1, kind: "done", title: "A", body: "完成" });
    useNotificationToastStore
      .getState()
      .push({ sessionId: 2, kind: "error", title: "B", body: "出错" });
    useNotificationToastStore.getState().dismiss(a);
    const { toasts } = useNotificationToastStore.getState();
    expect(toasts).toHaveLength(1);
    expect(toasts[0].sessionId).toBe(2);
  });

  it("超过上限丢弃最旧", () => {
    for (let i = 0; i < 7; i++) {
      useNotificationToastStore
        .getState()
        .push({ sessionId: i, kind: "done", title: String(i), body: "b" });
    }
    const { toasts } = useNotificationToastStore.getState();
    expect(toasts).toHaveLength(5);
    expect(toasts[0].title).toBe("2");
    expect(toasts[4].title).toBe("6");
  });
});
