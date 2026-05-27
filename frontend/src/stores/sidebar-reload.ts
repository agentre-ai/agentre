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
