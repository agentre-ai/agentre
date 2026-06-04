import { useCallback, useEffect, useState } from "react";

import { GroupLoad } from "../../wailsjs/go/app/App";
import type { app } from "../../wailsjs/go/models";
import { EventsOff, EventsOn } from "../../wailsjs/runtime/runtime";
import { useGroupStore } from "../stores/group-store";

// GroupLiveEvent 镜像后端往 group:event:<groupId> 推的载荷。kind 决定哪些字段
// 有值:kind="message" 带 message(GroupMessageItem 形状),kind="run_status"
// 带 runStatus。
type GroupLiveEvent = {
  kind: string;
  message?: app.GroupMessageItem;
  runStatus?: string;
};

// useGroup 负责单个群的「拉一次详情 + 订阅 live 事件」。详情统一落到
// useGroupStore,组件通过 hook 返回的 detail 读取,live 消息/状态由订阅写回 store。
export function useGroup(groupId: number) {
  const detail = useGroupStore((s) => s.details.get(groupId));
  const setDetail = useGroupStore((s) => s.setDetail);
  const appendMessage = useGroupStore((s) => s.appendMessage);
  const patchRunStatus = useGroupStore((s) => s.patchRunStatus);
  const [loading, setLoading] = useState(true);

  const reload = useCallback(async () => {
    setLoading(true);
    try {
      const d = await GroupLoad(groupId);
      setDetail(groupId, d);
    } finally {
      setLoading(false);
    }
  }, [groupId, setDetail]);

  useEffect(() => {
    void reload();
    // 订阅 effect 只 key 在 groupId + 稳定的 store actions 上(reload 已 useCallback),
    // 避免 callback 身份变化引起的重订阅抖动。
    const evt = `group:event:${groupId}`;
    EventsOn(evt, (payload: GroupLiveEvent) => {
      if (payload.kind === "message" && payload.message) {
        appendMessage(groupId, payload.message);
      }
      if (payload.kind === "run_status" && payload.runStatus) {
        patchRunStatus(groupId, payload.runStatus);
      }
    });
    return () => EventsOff(evt);
  }, [groupId, reload, appendMessage, patchRunStatus]);

  return { detail, loading, reload };
}
