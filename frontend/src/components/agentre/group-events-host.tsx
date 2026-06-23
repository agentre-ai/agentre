import * as React from "react";

import { useGroupListStore } from "@/stores/group-list-store";
import { useGroupStore } from "@/stores/group-store";
import { useSessionStatusStore } from "@/stores/session-status-store";
import { ensureSessionInSidebar } from "@/stores/sidebar-reload";

import { EventsOff, EventsOn } from "../../../wailsjs/runtime/runtime";

// GroupRunStateEvent 镜像后端往全局 groups:run_state 频道推的载荷
// (group_svc.GroupRunStateEventName):只有 run_status 与 member_run_state 两种,
// 都带 groupID;member_run_state 额外带 backingSessionID 映射到会话行。
type GroupRunStateEvent = {
  kind: string;
  groupID?: number;
  runStatus?: string;
  memberID?: number;
  runState?: string;
  backingSessionID?: number;
};

const GROUPS_RUN_STATE_EVENT = "groups:run_state";

// GroupEventsHost 是「无 DOM 的全局订阅器」(与 ChatStreamsHost 同型),挂在 App
// 顶层跨路由不 unmount。per-group 频道(group:event:<id>)只有打开的群页订阅,
// 侧栏(群行 + 成员 backing session 行)不开群页拿不到运行态 —— 这里订阅全局
// 频道补上:
//   - run_status → group-list-store(侧栏群行状态点) + group-store(群页缓存)
//   - member_run_state → session-status-store(backing session 会话行) +
//     group-store roster
// 群页打开时 use-group 与这里双写同一 store,各 patch 均同值短路,幂等。
export function GroupEventsHost(): React.ReactElement | null {
  React.useEffect(() => {
    EventsOn(GROUPS_RUN_STATE_EVENT, (ev: GroupRunStateEvent) => {
      if (!ev?.groupID) return;
      if (ev.kind === "run_status" && ev.runStatus) {
        useGroupListStore.getState().patchRunStatus(ev.groupID, ev.runStatus);
        useGroupStore.getState().patchRunStatus(ev.groupID, ev.runStatus);
        return;
      }
      if (ev.kind !== "member_run_state" || !ev.memberID || !ev.runState) {
        return;
      }
      useGroupStore
        .getState()
        .patchMemberRunState(ev.groupID, ev.memberID, ev.runState);
      const sid = ev.backingSessionID ?? 0;
      if (sid <= 0) return;
      const prev = useSessionStatusStore.getState().statuses.get(sid);
      if (ev.runState === "running") {
        useSessionStatusStore.getState().upsert(sid, {
          agentStatus: "running",
          needsAttention: prev?.needsAttention ?? false,
          permissionMode: prev?.permissionMode,
        });
        // backing session 是成员被 @ 那轮才惰性新建的, 群成员轮不经过
        // ChatPanel.onSidebarShouldReload —— 左栏 chat-agents-store 没人 reload,
        // 这一行进不了列表(行不在则 running 也无处挂)。这里若发现左栏还不认识它,
        // 补一次整刷把行带进来 + 点亮 agent 运行灯; 已收录则短路, 不每轮发 RPC。
        ensureSessionInSidebar(sid);
      } else if (prev?.agentStatus === "running") {
        // 调度器层面的 idle 只降级 running:turn 中可能翻 waiting(等审批)/error,
        // 覆盖掉会丢「需要你处理」的提示。
        useSessionStatusStore.getState().upsert(sid, {
          agentStatus: "idle",
          needsAttention: prev.needsAttention,
          permissionMode: prev.permissionMode,
        });
      }
    });
    return () => EventsOff(GROUPS_RUN_STATE_EVENT);
  }, []);
  return null;
}
