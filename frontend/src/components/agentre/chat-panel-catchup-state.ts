// chat-panel-catchup-state 存「这条会话有多少补齐内容用户还没看见」。
//
// 断连期间 daemon 照常落库,重连后按游标一次性重放 —— 转录区会在后台悄悄长出
// 一大截。位置不动是对的(用户可能正在翻历史),但他得知道下面多了什么、其中有没有
// 等着他回答的待决策,这份摘要就是那枚跳转控件的全部输入。
//
// 计量单位是**转录行**,不是通知条数。daemon 对每个 agentruntime 事件都落一行日志
// 并在重连后逐条重放,TextDelta / ThinkingDelta / UsageUpdate 全在内:断网两分钟、
// 期间 agent 流式吐一条长回复,重放的通知能上千条,而用户只多了一条助手消息。所以
// 这里在补齐窗口的两端各取一次转录行数(通道跌进重连时快照、补齐落定时做差),
// 重放的通知条数只当「这次重连确实漏掉了东西」的闸门,不进文案。
//
// 行数由挂载中的 ChatPanel 经 registerTranscriptRowCounter 供给 —— 它是唯一知道
// 转录长什么样的人。取数时机只有窗口两端各一次,所以现算即可,不用一直维护。
//
// 写入方只有 ChatStreamsHost:连接态那条流的每一发都经它翻译(reconnecting 开窗、
// connected 落定)。清除方只有 ChatPanel:用户回到底部(自己滚回去或点控件跳过去)
// 即视为看过了。
//
// 为什么不并进 session-conn-store:那里存的是「此刻通道通不通」,一帧一个值、会话
// 的流一结束就摘项;补齐摘要恰恰要活过 turn 结束 —— 用户可能过很久才切回这个 tab。
// 两者生命周期相反,合在一起必然互相清掉对方。

import * as React from "react";

export type CatchUpSummary = {
  // newRows 是累计值:用户回到底部前发生的每一次补齐都往上加,不然第二次补齐
  // 会把第一次的行数抹掉,控件报的数就比实际少。
  //
  // 它可以是 0 —— 补齐把上千条 delta 全追加进了**已经存在**的那一行(还在流的
  // 助手消息),行数没变但内容确实变多了。0 只代表「数不出新行」,不代表没有新内容:
  // 控件照常浮出(R14:补齐产生了新内容且用户不在底部时给一条回去的路),只是不报
  // 一个撑不住的数字。
  newRows: number;
  // pendingDecisions 取最新快照(它是「补完后还剩几个没回答」的绝对值,不是增量)。
  pendingDecisions: number;
};

const summaries = new Map<number, CatchUpSummary>();
// windows:补齐窗口打开那一刻的转录行数快照。有条目 = 这条会话正处在断连窗口里。
const windows = new Map<number, number>();
const rowCounters = new Map<number, () => number>();
const listeners = new Set<() => void>();

// registerTranscriptRowCounter 由挂载中的 ChatPanel 注册「本会话此刻有多少转录行」。
// 返回注销函数。没有面板挂载(tab 没开)时数不出行数,补齐落定按 0 新行处理 ——
// 那种情况下用户下次打开这个 tab 本就落在底部,控件随即销账。
export function registerTranscriptRowCounter(
  sessionId: number,
  count: () => number,
): () => void {
  rowCounters.set(sessionId, count);
  return () => {
    // 只摘自己那一份:会话切换时新面板已经注册过了,旧面板卸载不该把它清掉。
    if (rowCounters.get(sessionId) === count) rowCounters.delete(sessionId);
  };
}

function currentRowCount(sessionId: number): number | null {
  const count = rowCounters.get(sessionId);
  return count ? count() : null;
}

// openCatchUpWindow 记下补齐窗口打开那一刻的转录行数 —— 通道跌出 connected 那一发。
// 窗口已开就不再重取:一次断连期间连接态可能反复跳(reconnecting → lost),重取会
// 把已经补进来的那一段算丢。
export function openCatchUpWindow(sessionId: number): void {
  if (windows.has(sessionId)) return;
  const rows = currentRowCount(sessionId);
  if (rows === null) return;
  windows.set(sessionId, rows);
}

// recordCatchUp 记一次补齐落定。replayedNotifications 只当闸门:<= 0 表示这次重连
// 什么都没漏掉,不产生新内容也就不该浮出控件 —— 直接不留摘要,而不是留一条空壳。
// 用户看到的数字来自窗口两端的行数差,与重放了多少条通知无关。
export function recordCatchUp(
  sessionId: number,
  replayedNotifications: number,
  pendingDecisions: number,
): void {
  const baseline = windows.get(sessionId);
  windows.delete(sessionId);
  if (replayedNotifications <= 0) return;
  const after = currentRowCount(sessionId);
  const added =
    baseline === undefined || after === null
      ? 0
      : Math.max(0, after - baseline);
  const prev = summaries.get(sessionId);
  summaries.set(sessionId, {
    newRows: (prev?.newRows ?? 0) + added,
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

// 测试隔离用,生产代码不该调。监听者与行数取数口都不清 —— 清了会让仍挂载的组件失联。
export function __resetCatchUpStateForTesting(): void {
  summaries.clear();
  windows.clear();
}
