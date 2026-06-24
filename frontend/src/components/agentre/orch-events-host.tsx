import * as React from "react";

import { useOrchRunListStore } from "../../stores/orch-run-list-store";
import { useOrchRunStore } from "../../stores/orch-run-store";

import { EventsOff, EventsOn } from "../../../wailsjs/runtime/runtime";

import { ORCH_EVENTS } from "./orchestration/events";

// OrchEventsHost 是「无 DOM 的全局订阅器」,挂在 App 顶层跨路由不 unmount。
// 订阅全部 6 个 orch:run:* 事件,收到后:
//   1. 转发给 orch-run-store.onRunEvent(name, payload) → 更新详情/死锁周期
//   2. 触发 orch-run-list-store.load()                  → 刷新列表运行态
export function OrchEventsHost(): React.ReactElement | null {
  React.useEffect(() => {
    // 收集 6 个事件的反订阅函数
    const unsubs = Object.values(ORCH_EVENTS).map((name) =>
      EventsOn(name, (payload: { runId: number; cycle?: number[] }) => {
        useOrchRunStore.getState().onRunEvent(name, payload);
        void useOrchRunListStore.getState().load();
      }),
    );

    return () => {
      // 先调用各 EventsOn 返回的 unsub(如果 runtime 支持)
      unsubs.forEach((unsub) => {
        if (typeof unsub === "function") unsub();
      });
      // 再用 EventsOff 兜底
      Object.values(ORCH_EVENTS).forEach((name) => EventsOff(name));
    };
  }, []);

  return null;
}
