import * as React from "react";

import { ListAgentExecTargetAvailability } from "@/../wailsjs/go/app/App";
import type { chat_svc } from "@/../wailsjs/go/models";

// useExecTargetAvailability 逐档判定一个 Agent 的执行目标列表可用性（R15），供
// 组织架构页展示每档的「当前生效 / 在线 / 离线 / 未配对 / 需指定 LLM Provider…」
// 徽标。projectID 固定传 0——Agent 详情页不绑定任何会话/项目，不受「该机器上有没有
// 配这个项目的路径」这一项约束（那条只在 R15a 的会话内改选场景生效）。
//
// 返回一个按 agentBackendId 索引的 Map，而不是原始顺序数组：Agent 详情页里的执行
// 目标列表可能刚被本地拖拽重排、还没保存完成，这份数据仍然是「保存前最后一次成功
// 读取」的快照——按 id 查找让重排（不改变 backend 集合）不需要等一轮保存往返就能
// 复用已有的可用性判定，调用方只在「新增/删除了哪个 backend」时才需要拿到新数据
// （用 targetsKey 控制何时重新拉取）。
export function useExecTargetAvailability(agentId: number, targetsKey: string) {
  const [byBackendId, setByBackendId] = React.useState<
    Map<number, chat_svc.ExecTargetAvailabilityView>
  >(new Map());
  const [loading, setLoading] = React.useState(false);
  // 只有最新一次请求可以写状态：增删执行目标会连着触发多次拉取，先发的那次晚返回时
  // 不能把新一批判定盖回旧的（徽标会一直停在删掉那一档还在的快照上）。卸载时把代次
  // 推进一格，在飞的请求回来后不再写已卸载组件的状态。
  const reqRef = React.useRef(0);

  const reload = React.useCallback(async () => {
    const req = ++reqRef.current;
    if (!agentId) {
      setByBackendId(new Map());
      return;
    }
    setLoading(true);
    try {
      const resp = await ListAgentExecTargetAvailability(agentId, 0);
      if (req !== reqRef.current) return;
      setByBackendId(new Map((resp ?? []).map((s) => [s.agentBackendId, s])));
    } catch (e) {
      if (req !== reqRef.current) return;
      console.error("[org] exec target availability load failed", e);
    } finally {
      if (req === reqRef.current) setLoading(false);
    }
    // targetsKey 只用来判断"这一批 backend id 是否变了"，值本身不进请求体。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agentId, targetsKey]);

  React.useEffect(() => {
    void reload();
  }, [reload]);

  React.useEffect(
    () => () => {
      reqRef.current++;
    },
    [],
  );

  return { byBackendId, loading, reload };
}
