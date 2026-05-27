// 锁死 useChatStream 的稳定订阅契约：onEvent 变化时不能 unsubscribe/resubscribe，
// 否则 ChatStreamsHost 每次 streams Map 引用变更（每条 chunk delta 都会）都会重订阅，
// 落在抖动窗口内的事件（典型如 steer_consumed）会被 Wails EventsOff 直接丢掉，
// 表现为「chip 不清、用户消息也不插」。
import { renderHook, act } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";

import { useChatStream } from "./use-chat-stream";

import type { ChatStreamEvent } from "./use-chat-stream";

vi.mock("../../wailsjs/runtime/runtime", () => ({
  // Wails EventsOn 返回 () => void 卸载函数;mock 默认返回 vi.fn() 以便断言卸载次数。
  EventsOn: vi.fn(() => vi.fn()),
}));

import { EventsOn } from "../../wailsjs/runtime/runtime";

const onEvents = EventsOn as ReturnType<typeof vi.fn>;

describe("useChatStream", () => {
  beforeEach(() => {
    onEvents.mockReset();
    onEvents.mockImplementation(() => vi.fn());
  });

  it("subscribes once per stream name and never re-subscribes when onEvent identity changes", () => {
    const cb1 = vi.fn();
    const { rerender } = renderHook(
      ({ stream, onEvent }: { stream: string; onEvent: typeof cb1 }) =>
        useChatStream(stream, onEvent),
      { initialProps: { stream: "chat:stream:1:2", onEvent: cb1 } },
    );

    expect(onEvents).toHaveBeenCalledTimes(1);
    const off1 = onEvents.mock.results[0].value as ReturnType<typeof vi.fn>;
    expect(off1).not.toHaveBeenCalled();

    // 父组件重渲染传入新 arrow function (典型 ChatStreamsHost 每 chunk 一次)
    const cb2 = vi.fn();
    rerender({ stream: "chat:stream:1:2", onEvent: cb2 });
    const cb3 = vi.fn();
    rerender({ stream: "chat:stream:1:2", onEvent: cb3 });

    // 关键：仍然只订阅过 1 次 (卸载函数也从未被调用,没有抖动窗口)
    expect(onEvents).toHaveBeenCalledTimes(1);
    expect(off1).not.toHaveBeenCalled();
  });

  it("the registered listener always dispatches to the LATEST onEvent (ref keeps callback fresh)", () => {
    const cb1 = vi.fn();
    const { rerender } = renderHook(
      ({ stream, onEvent }: { stream: string; onEvent: typeof cb1 }) =>
        useChatStream(stream, onEvent),
      { initialProps: { stream: "chat:stream:1:2", onEvent: cb1 } },
    );
    const registered = onEvents.mock.calls[0][1] as (
      e: ChatStreamEvent,
    ) => void;

    const cb2 = vi.fn();
    rerender({ stream: "chat:stream:1:2", onEvent: cb2 });

    act(() => {
      registered({ kind: "done" });
    });

    // 旧 onEvent 不再被调用，新的被调用 — ref 起作用，订阅未抖动
    expect(cb1).not.toHaveBeenCalled();
    expect(cb2).toHaveBeenCalledTimes(1);
  });

  it("re-subscribes when stream name actually changes, calling per-listener cleanup", () => {
    const cb = vi.fn();
    const { rerender } = renderHook(
      ({ stream }: { stream: string }) => useChatStream(stream, cb),
      { initialProps: { stream: "chat:stream:1:2" } },
    );
    expect(onEvents).toHaveBeenCalledTimes(1);
    const off1 = onEvents.mock.results[0].value as ReturnType<typeof vi.fn>;

    rerender({ stream: "chat:stream:1:5" });
    expect(off1).toHaveBeenCalledTimes(1);
    expect(onEvents).toHaveBeenCalledTimes(2);
    expect(onEvents.mock.calls[1][0]).toBe("chat:stream:1:5");
  });

  it("noop when stream is null", () => {
    const cb = vi.fn();
    renderHook(() => useChatStream(null, cb));
    expect(onEvents).not.toHaveBeenCalled();
  });
});
