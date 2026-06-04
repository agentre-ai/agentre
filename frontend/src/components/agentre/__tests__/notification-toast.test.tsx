import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (k: string) => k }),
}));

import { NotificationToastViewport } from "../notification-toast";
import { useChatTabsStore } from "../../../stores/chat-tabs-store";
import { useNotificationToastStore } from "../../../stores/notification-toast-store";

beforeEach(() => {
  vi.clearAllMocks();
  useNotificationToastStore.getState().clear();
});
afterEach(() => {
  vi.useRealTimers();
});

describe("NotificationToastViewport", () => {
  it("渲染推入的 toast(会话名 + 状态文案)", () => {
    render(<NotificationToastViewport />);
    act(() => {
      useNotificationToastStore.getState().push({
        sessionId: 42,
        kind: "done",
        title: "重构 chat_svc",
        body: "会话已完成",
      });
    });
    expect(screen.getByText("重构 chat_svc")).toBeInTheDocument();
    expect(screen.getByText("会话已完成")).toBeInTheDocument();
  });

  it("点「跳转到会话」→ openSession(sessionId) 且移除该条", async () => {
    const openSession = vi.fn();
    useChatTabsStore.setState({ openSession });
    render(<NotificationToastViewport />);
    act(() => {
      useNotificationToastStore
        .getState()
        .push({ sessionId: 42, kind: "done", title: "T", body: "B" });
    });
    await userEvent.click(
      screen.getByRole("button", { name: "notify.openSession" }),
    );
    expect(openSession).toHaveBeenCalledWith(42);
    expect(useNotificationToastStore.getState().toasts).toHaveLength(0);
  });

  it("点关闭 → 移除该条", async () => {
    render(<NotificationToastViewport />);
    act(() => {
      useNotificationToastStore
        .getState()
        .push({ sessionId: 1, kind: "error", title: "T", body: "B" });
    });
    await userEvent.click(
      screen.getByRole("button", { name: "notify.dismiss" }),
    );
    expect(useNotificationToastStore.getState().toasts).toHaveLength(0);
  });

  it("done 到时自动消失", () => {
    vi.useFakeTimers();
    render(<NotificationToastViewport />);
    act(() => {
      useNotificationToastStore
        .getState()
        .push({ sessionId: 1, kind: "done", title: "T", body: "B" });
    });
    expect(useNotificationToastStore.getState().toasts).toHaveLength(1);
    act(() => {
      vi.advanceTimersByTime(6000);
    });
    expect(useNotificationToastStore.getState().toasts).toHaveLength(0);
  });

  it("waiting / error 不自动消失(需用户处理)", () => {
    vi.useFakeTimers();
    render(<NotificationToastViewport />);
    act(() => {
      useNotificationToastStore
        .getState()
        .push({ sessionId: 1, kind: "waiting", title: "T", body: "B" });
    });
    act(() => {
      vi.advanceTimersByTime(30000);
    });
    expect(useNotificationToastStore.getState().toasts).toHaveLength(1);
  });
});
