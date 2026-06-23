import { act, render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useChatStreamsStore } from "@/stores/chat-streams-store";

import { ChatStreamsHost } from "../chat-streams-host";

// 用计数器替身代替真实 StreamSubscriber:它每被 host 重渲染/重新挂载一次就自增。
// 这样可以直接观测「host 因 store 变化重渲染了几次订阅树」,而不依赖真实的
// Wails EventsOn 订阅副作用。
const subscriberRenders = vi.hoisted(() => ({ count: 0 }));
vi.mock("../stream-subscriber", () => ({
  StreamSubscriber: () => {
    subscriberRenders.count++;
    return null;
  },
}));

function resetStores() {
  useChatStreamsStore.setState({ streams: new Map() });
  subscriberRenders.count = 0;
}

describe("ChatStreamsHost re-render isolation", () => {
  beforeEach(() => {
    resetStores();
  });

  it("does not re-render the subscriber tree when only liveDelta changes (chunk)", () => {
    // 一条活跃流。host 只需要它的 {sessionId,name} 身份来挂订阅 + 路由事件,
    // 这两者在整条流生命周期里都不变。
    useChatStreamsStore.getState().openStream({
      assistantMessageId: 1001,
      name: "chat:event:42:1001",
      sessionId: 42,
      streamStartedAt: 1700000000000,
    });

    render(<ChatStreamsHost />);
    const afterMount = subscriberRenders.count;
    expect(afterMount).toBeGreaterThan(0); // 挂载渲染过一次

    // 模拟连续 chunk:每次只改 liveDelta(appendLiveText 会重建 streams Map)。
    act(() => {
      useChatStreamsStore.getState().appendLiveText(42, "a");
    });
    act(() => {
      useChatStreamsStore.getState().appendLiveText(42, "b");
    });
    act(() => {
      useChatStreamsStore.getState().appendLiveText(42, "c");
    });

    // 身份集合(sessionId + name)没变 → host 不该重渲染订阅树 → 计数不增。
    expect(subscriberRenders.count).toBe(afterMount);
  });

  it("re-renders the subscriber tree when a stream is added (identity changes)", () => {
    render(<ChatStreamsHost />);
    const base = subscriberRenders.count;

    act(() => {
      useChatStreamsStore.getState().openStream({
        assistantMessageId: 1,
        name: "chat:event:7:1",
        sessionId: 7,
        streamStartedAt: 1700000000000,
      });
    });

    // 新流加入 = 身份集合变化 → host 必须重渲染挂上新订阅。
    expect(subscriberRenders.count).toBeGreaterThan(base);
  });
});
