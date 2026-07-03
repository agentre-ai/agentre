import * as React from "react";

import { useOrchRunListStore } from "../../stores/orch-run-list-store";
import { useOrchRunStore } from "../../stores/orch-run-store";
import { reloadSidebarSources } from "../../stores/sidebar-reload";

import { EventsOff, EventsOn } from "../../../wailsjs/runtime/runtime";

import { ORCH_EVENTS } from "./orchestration/events";

// OrchEventsHost 是「无 DOM 的全局订阅器」,挂在 App 顶层跨路由不 unmount。
// 订阅全部 orch:run:* 事件,收到后:
//   1. 转发给 orch-run-store.onRunEvent(name, payload) → 更新详情/死锁周期
//   2. 触发 orch-run-list-store.load()                  → 刷新列表运行态
//   3. reloadSidebarSources()                          → 刷新左侧项目会话侧栏
//      编排 dispatch / ask 会「带外」新建子会话(带 ProjectID),这些会话绕开了
//      ChatPanel.onSidebarShouldReload,不刷侧栏就看不到新行。两个 store 各自
//      做 inflight dedup + 同值短路,按事件频率调用不会真触发多次 RPC。
export function OrchEventsHost(): React.ReactElement | null {
  React.useEffect(() => {
    // 收集各事件的反订阅函数
    const unsubs = Object.values(ORCH_EVENTS).map((name) =>
      EventsOn(name, (payload: { runId: number; cycle?: number[] }) => {
        useOrchRunStore.getState().onRunEvent(name, payload);
        void useOrchRunListStore.getState().load();
        reloadSidebarSources();
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
