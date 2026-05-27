import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useChatTabsStore } from "@/stores/chat-tabs-store";

import { useAttentionBump } from "../use-attention-bump";

describe("useAttentionBump", () => {
  beforeEach(() => {
    useChatTabsStore.setState({ tabs: [], activeTabId: null });
  });

  it("tab 从非 attention → attention 时调一次 bumpToAfterPinned", () => {
    useChatTabsStore.getState().openSessionInNewTab(1);
    useChatTabsStore.getState().openSessionInNewTab(2);
    const t2Id = useChatTabsStore.getState().tabs[1].id;

    const spy = vi.spyOn(useChatTabsStore.getState(), "bumpToAfterPinned");
    const { rerender } = renderHook(
      ({ ids }: { ids: Set<string> }) => useAttentionBump(ids),
      { initialProps: { ids: new Set<string>() } },
    );
    rerender({ ids: new Set([t2Id]) });
    expect(spy).toHaveBeenCalledWith(t2Id);
    expect(spy).toHaveBeenCalledTimes(1);
  });

  it("attention 持续 true 不重复触发", () => {
    useChatTabsStore.getState().openSessionInNewTab(1);
    const t1Id = useChatTabsStore.getState().tabs[0].id;
    const spy = vi.spyOn(useChatTabsStore.getState(), "bumpToAfterPinned");
    const { rerender } = renderHook(
      ({ ids }: { ids: Set<string> }) => useAttentionBump(ids),
      { initialProps: { ids: new Set([t1Id]) } },
    );
    spy.mockClear(); // 清除初始化时的调用
    rerender({ ids: new Set([t1Id]) });
    rerender({ ids: new Set([t1Id]) });
    expect(spy).toHaveBeenCalledTimes(0); // 持续在集合中不重复触发
  });

  it("true → false → true 时再次触发", () => {
    useChatTabsStore.getState().openSessionInNewTab(1);
    const t1Id = useChatTabsStore.getState().tabs[0].id;
    const spy = vi.spyOn(useChatTabsStore.getState(), "bumpToAfterPinned");
    const { rerender } = renderHook(
      ({ ids }: { ids: Set<string> }) => useAttentionBump(ids),
      { initialProps: { ids: new Set([t1Id]) } },
    );
    spy.mockClear(); // 清除初始化时的调用
    rerender({ ids: new Set<string>() });
    rerender({ ids: new Set([t1Id]) });
    expect(spy).toHaveBeenCalledTimes(1); // 再进入时触发一次
  });
});
