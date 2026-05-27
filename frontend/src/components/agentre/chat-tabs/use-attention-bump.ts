import * as React from "react";

import { useChatTabsStore } from "@/stores/chat-tabs-store";

// useAttentionBump: 监听 attentionTabIds 集合的边沿变化。
// 任一 id 从"上一帧不在集合"变为"这一帧在集合", 调一次 bumpToAfterPinned
// 把它搬到 pinned 前缀之后。持续在集合中不重复触发; 离开再回来重新触发。
export function useAttentionBump(attentionTabIds: Set<string>): void {
  const prev = React.useRef<Set<string>>(new Set());
  React.useEffect(() => {
    const bump = useChatTabsStore.getState().bumpToAfterPinned;
    for (const id of attentionTabIds) {
      if (!prev.current.has(id)) bump(id);
    }
    prev.current = new Set(attentionTabIds);
  }, [attentionTabIds]);
}
