// isResolvedAskState 判定一条 AskUserQuestion patch 是否处于终态(已答 / 已跳过 /
// 已失效)。终态 patch 应走 markAskUserQuestionAnswered 按 requestId 合并进在屏卡片,
// 而非 appendLiveAskUserQuestion 盲目追加(后者会留下重复卡、且不翻转原等待卡)。
//
// expired 漏判正是一个真实回归:turn finalize 对未答 AskUserQuestion 发出的失效锁定
// patch(answered=false && skipped=false && expired=true)若不算终态,就会走 append
// 分支 —— 在屏等待卡不翻转,只能等 turn 结束后的 reload 才显示「已失效」。
//
// 抽成无重依赖的纯函数,既给路由判定一个可单测的回归守卫,也让 host 的事件分派保持
// 「event → store action」的薄翻译。
export function isResolvedAskState(ask: {
  answered?: boolean;
  skipped?: boolean;
  expired?: boolean;
}): boolean {
  return !!(ask.answered || ask.skipped || ask.expired);
}
