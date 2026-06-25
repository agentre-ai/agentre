import { render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

// per-file mock: 不能用全局 alias(会破坏 App.test/foundation.test 的 importActual)
vi.mock("../../../../wailsjs/runtime/runtime", () => ({
  EventsOn: vi.fn(),
  EventsOff: vi.fn(),
}));

// 拦截 wailsjs go 绑定(stores 内部 import)
vi.mock("../../../../wailsjs/go/app/App", () => ({
  RunLoad: vi.fn().mockResolvedValue({}),
  RunList: vi.fn().mockResolvedValue([]),
}));

import { useOrchRunListStore } from "@/stores/orch-run-list-store";
import { useOrchRunStore } from "@/stores/orch-run-store";
import { EventsOff, EventsOn } from "../../../../wailsjs/runtime/runtime";
import { OrchEventsHost } from "../orch-events-host";
import { ORCH_EVENTS } from "../orchestration/events";

type Handler = (payload: { runId: number; cycle?: number[] }) => void;

// 渲染组件并返回「已注册的事件→回调」映射
function mountHostAndGetHandlers(): Map<string, Handler> {
  const handlers = new Map<string, Handler>();
  (EventsOn as ReturnType<typeof vi.fn>).mockImplementation(
    (evt: string, h: Handler) => {
      handlers.set(evt, h);
      // 返回一个 unsub 函数(runtime 通常返回此函数)
      return () => {};
    },
  );
  render(<OrchEventsHost />);
  return handlers;
}

describe("OrchEventsHost", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    useOrchRunStore.getState().__reset();
    useOrchRunListStore.getState().__reset();
  });

  it("订阅全部 6 个 orch:run:* 事件", () => {
    const handlers = mountHostAndGetHandlers();
    // 断言每个事件都被订阅
    for (const name of Object.values(ORCH_EVENTS)) {
      expect(handlers.has(name), `OrchEventsHost 必须订阅事件 ${name}`).toBe(
        true,
      );
    }
  });

  it("收到 orch:run:updated 时转发 onRunEvent 并触发 load", async () => {
    const handlers = mountHostAndGetHandlers();

    // spy on store actions
    const onRunEvent = vi.spyOn(useOrchRunStore.getState(), "onRunEvent");
    const load = vi
      .spyOn(useOrchRunListStore.getState(), "load")
      .mockResolvedValue();

    const payload = { runId: 42 };
    const h = handlers.get(ORCH_EVENTS.updated);
    expect(h, "必须订阅 orch:run:updated").toBeDefined();
    h!(payload);

    expect(onRunEvent).toHaveBeenCalledWith(ORCH_EVENTS.updated, payload);
    expect(load).toHaveBeenCalledTimes(1);
  });

  it("收到 orch:run:deadlock 时转发 onRunEvent(含 cycle)并触发 load", async () => {
    const handlers = mountHostAndGetHandlers();

    const onRunEvent = vi.spyOn(useOrchRunStore.getState(), "onRunEvent");
    const load = vi
      .spyOn(useOrchRunListStore.getState(), "load")
      .mockResolvedValue();

    const payload = { runId: 7, cycle: [1, 2, 3] };
    const h = handlers.get(ORCH_EVENTS.deadlock);
    expect(h, "必须订阅 orch:run:deadlock").toBeDefined();
    h!(payload);

    expect(onRunEvent).toHaveBeenCalledWith(ORCH_EVENTS.deadlock, payload);
    expect(load).toHaveBeenCalledTimes(1);
  });

  it("unmount 时调用 EventsOff 对全部事件解绑", () => {
    const { unmount } = render(<OrchEventsHost />);
    (EventsOff as ReturnType<typeof vi.fn>).mockClear();
    unmount();

    // EventsOff 至少被调用了 6 次(每个事件一次)
    const offCalls = (EventsOff as ReturnType<typeof vi.fn>).mock.calls.map(
      (c) => c[0],
    );
    for (const name of Object.values(ORCH_EVENTS)) {
      expect(offCalls, `EventsOff 未被以 ${name} 调用`).toContain(name);
    }
  });
});
