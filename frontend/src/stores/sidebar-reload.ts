// frontend/src/stores/sidebar-reload.ts
//
// reloadSidebarSources 是「需要全栈刷新左侧 sidebar 数据」的统一入口。
// chat-panel ChatPanel.onSidebarShouldReload (新建会话 / turn done / steer 等)
// 都通过这一处触发, 同步把 /chat 的 chat-agents-store 与 /projects 的
// project-sessions-store 都 reload 一遍。两个 store 各自做 inflight dedup +
// 同值短路, 调用频繁也不会真触发多次 RPC。
//
// 把这个 helper 单独抽出来的好处:
//   - 调用方 (chat-panel-host) 一行调用即可, 不需要 import 两个 store。
//   - 后续再加新数据源 (例如 issues 侧栏) 只改这里, 不动散落各处的 callback。
//   - 测试: 可以一次断言这条统一信号确实把两个 store 都刷新了。

import { useChatAgentsStore } from "./chat-agents-store";
import { useProjectSessionsStore } from "./project-sessions-store";

export function reloadSidebarSources(): void {
  void useChatAgentsStore.getState().reload();
  void useProjectSessionsStore.getState().reload();
}

// isSessionKnownToSidebar 判断某 session 是否已经被左栏收录。chat-agents-store 是
// 「agent → 其会话」的唯一索引 (sessionIds 是去重后的全量 session id), 群聊成员的
// backing session 也归在对应成员 agent 名下, 所以这里只需问 chat-agents-store。
export function isSessionKnownToSidebar(sessionId: number): boolean {
  if (sessionId <= 0) return false;
  for (const a of useChatAgentsStore.getState().agents) {
    if (a.sessionIds.includes(sessionId)) return true;
  }
  return false;
}

// ensureSessionInSidebar 保证「带外创建」的会话能进左栏: 若该 session 还没被左栏
// 收录, 触发一次 reloadSidebarSources() 把它整刷进来; 已收录则跳过, 避免每轮都发
// 无谓 RPC。
//
// 为什么需要它: 普通单聊靠 ChatPanel.onSidebarShouldReload 在 turn 起手/落定时
// reload 左栏。但有些会话是在 ChatPanel 之外被创建的 —— 当前是群聊成员被 @ 那轮
// 才惰性新建的 backing session (group-events-host 收到 member_run_state running
// 时调用), 后续也可能是「远程调用创建会话」等路径。这些都绕开了 onSidebarShouldReload,
// 左栏拿不到新行 (行不在 → 列表里没有, running 状态也无处挂)。统一走这一个入口即可。
export function ensureSessionInSidebar(sessionId: number): void {
  if (sessionId <= 0) return;
  if (isSessionKnownToSidebar(sessionId)) return;
  reloadSidebarSources();
}
