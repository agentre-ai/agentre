// chat-panel-catchup-state 存「这条会话有多少补齐内容用户还没看见」。
//
// 断连期间 daemon 照常落库,重连后按游标一次性重放 —— 转录区会在后台悄悄长出
// 一大截。位置不动是对的(用户可能正在翻历史),但他得知道下面多了什么、其中有没有
// 等着他回答的待决策,这份摘要就是那枚跳转控件的全部输入。
//
// 写入方只有 ChatStreamsHost:补齐落定那一发 connection_state 带回 caughtUpCount /
// pendingDecisions。清除方只有 ChatPanel:用户回到底部(自己滚回去或点控件跳过去)
// 即视为看过了。
//
// 为什么不并进 session-conn-store:那里存的是「此刻通道通不通」,一帧一个值、会话
// 的流一结束就摘项;补齐摘要恰恰要活过 turn 结束 —— 用户可能过很久才切回这个 tab。
// 两者生命周期相反,合在一起必然互相清掉对方。

import * as React from "react";

export type CatchUpSummary = {
  // newItems 是累计值:用户回到底部前发生的每一次补齐都往上加,不然第二次补齐
  // 会把第一次的条数抹掉,控件报的数就比实际少。
  newItems: number;
  // pendingDecisions 取最新快照(它是「补完后还剩几个没回答」的绝对值,不是增量)。
  pendingDecisions: number;
};

const summaries = new Map<number, CatchUpSummary>();
const listeners = new Set<() => void>();

// recordCatchUp 记一次补齐落定。caughtUpCount <= 0 表示这次重连什么都没漏掉,
// 不产生新内容也就不该浮出控件 —— 直接不留摘要,而不是留一条 newItems=0 的空壳。
export function recordCatchUp(
  sessionId: number,
  caughtUpCount: number,
  pendingDecisions: number,
): void {
  if (caughtUpCount <= 0) return;
  const prev = summaries.get(sessionId);
  summaries.set(sessionId, {
    newItems: (prev?.newItems ?? 0) + caughtUpCount,
    pendingDecisions,
  });
  emit();
}

export function clearCatchUp(sessionId: number): void {
  if (!summaries.delete(sessionId)) return;
  emit();
}

export function getCatchUp(sessionId: number): CatchUpSummary | null {
  return summaries.get(sessionId) ?? null;
}

function emit(): void {
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

// useCatchUpSummary 是组件侧读法。getCatchUp 返回的是 Map 里那个对象本身
// (没变就是同一引用),useSyncExternalStore 才不会把每次读当成「store 又变了」。
export function useCatchUpSummary(sessionId: number): CatchUpSummary | null {
  const getSnapshot = React.useCallback(
    () => getCatchUp(sessionId),
    [sessionId],
  );
  return React.useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}

// 测试隔离用,生产代码不该调。监听者不清 —— 清了会让仍挂载的组件失联。
export function __resetCatchUpStateForTesting(): void {
  summaries.clear();
}
