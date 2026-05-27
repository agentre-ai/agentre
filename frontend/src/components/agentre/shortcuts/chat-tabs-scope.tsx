import * as React from "react";

import { useChatTabsStore } from "@/stores/chat-tabs-store";

import { useTabsView } from "../chat-tabs/use-tabs-view";
import { useShortcutsContext } from "./shortcuts-provider";

// ChatTabsShortcuts —— 把 ⌘1..9 绑到 sortedTabs 的第 N 个 Tab，⌘W 关闭当前 Tab。
// 挂在 <ShortcutsProvider> 内部（AppLayout 里），对所有路由全局生效。
// 通过 setTabsScope 把 switchTo / close 两个操作注入到 ShortcutsProvider 的
// tabsScopeRef 槽位，dispatch 时不再走旧的 SessionScope / attention 路径。
export function ChatTabsShortcuts(): null {
  const sortedTabs = useTabsView();
  const setActive = useChatTabsStore((s) => s.setActive);
  const closeTab = useChatTabsStore((s) => s.closeTab);
  const activeTabId = useChatTabsStore((s) => s.activeTabId);
  const { setTabsScope } = useShortcutsContext();

  React.useEffect(() => {
    setTabsScope({
      switchTo: (idx: number) => {
        const target = sortedTabs[idx];
        if (target) setActive(target.id);
      },
      close: () => {
        if (activeTabId) closeTab(activeTabId);
      },
    });
    return () => {
      setTabsScope(null);
    };
  }, [sortedTabs, activeTabId, setActive, closeTab, setTabsScope]);

  return null;
}
