import { useChatStream } from "@/hooks/use-chat-stream";

type ChatStreamEvent = Parameters<Parameters<typeof useChatStream>[1]>[0];

// StreamSubscriber 是一个不可见的"订阅器"：把单个 Wails 事件流的生命周期
// （EventsOn / EventsOff）绑在自己 mount / unmount 上。
// ChatPage 用一组 <StreamSubscriber key={sessionId} ... /> 来并行订阅多个会话的流，
// 切会话不会丢订阅；某条流 done/closed 时把对应 entry 从父端 Map 删掉即可自动 EventsOff。
export function StreamSubscriber({
  streamName,
  onEvent,
}: {
  streamName: string;
  onEvent: (ev: ChatStreamEvent) => void;
}): null {
  useChatStream(streamName, onEvent);
  return null;
}
