import { Hourglass, Lock, Trash2, X } from "lucide-react";

import { cn } from "@/lib/utils";

// QueuedItem 与后端 EnqueueResponse.queuedId/cancellable 一一对应。
// text 是用户输入的原文（service 端会 TrimSpace），前端用于在 chip 上显示预览 + tooltip。
export type QueuedItem = {
  id: string;
  text: string;
  /** 该条目所在 backend 是否实现 SteerCanceler。codex=false → X 替换为锁图标。 */
  cancellable: boolean;
};

type Props = {
  queued: QueuedItem[];
  /** 用户点单条 chip 上的 X。父组件负责调 CancelQueuedChatMessage 并同步本地 state。 */
  onCancel: (id: string) => void;
  /** 用户点 header「清空」。父组件负责调 CancelQueuedChatMessage({queuedId: ""})。 */
  onClearAll: () => void;
};

// QueuedMessagesBar 显示当前 session 还没被 AI 取走的排队消息。
// 挂在 ChatComposer 内的 card 顶部（编辑模式 banner 同位），样式上用 muted
// 区分于 input 主区，让用户一眼看到「这是缓冲，不是正常输入」。
//
// 空列表时 render null —— 这是组件契约：父组件不必判空，QueuedMessagesBar
// 自己决定可见性。这样 ChatComposer 的 queueSlot prop 一直传 <QueuedMessagesBar/>
// 也不会留空 DOM。
export function QueuedMessagesBar({ queued, onCancel, onClearAll }: Props) {
  if (queued.length === 0) return null;
  const anyCancellable = queued.some((q) => q.cancellable);

  return (
    <div
      role="region"
      aria-label="排队中的消息"
      className="flex flex-col gap-1.5 border-b border-border bg-muted px-3 py-2"
    >
      <div className="flex items-center gap-2">
        <Hourglass
          className="size-3 shrink-0 text-muted-foreground"
          aria-hidden="true"
        />
        <span className="text-2xs font-semibold text-foreground">
          排队中 · {queued.length} 条
        </span>
        <span className="text-2xs text-muted-foreground">
          {anyCancellable ? "AI 完成后插入" : "codex 不支持撤回"}
        </span>
        <div className="min-w-0 flex-1" />
        <button
          type="button"
          disabled={!anyCancellable}
          aria-label="清空排队消息"
          title={anyCancellable ? "清空全部排队消息" : "codex 后端不支持撤回"}
          onClick={onClearAll}
          className={cn(
            "inline-flex h-6 cursor-pointer items-center gap-1 rounded-sm border border-border-strong px-2 text-2xs font-medium transition-colors",
            "hover:bg-accent hover:text-foreground",
            "disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent",
          )}
        >
          <Trash2 className="size-3" aria-hidden="true" />
          清空
        </button>
      </div>
      <ul className="flex flex-col gap-1">
        {queued.map((q) => (
          <li
            key={q.id}
            className="flex items-center gap-2 rounded-sm border border-border bg-card px-2 py-1"
            title={q.text}
          >
            <Hourglass
              className="size-3 shrink-0 text-muted-foreground"
              aria-hidden="true"
            />
            <span className="min-w-0 flex-1 truncate text-xs text-foreground">
              {q.text}
            </span>
            {q.cancellable ? (
              <button
                type="button"
                aria-label="撤回这条排队消息"
                title="撤回 (尚未被 AI 取走时有效)"
                onClick={() => onCancel(q.id)}
                className="inline-flex size-5 shrink-0 cursor-pointer items-center justify-center rounded-sm text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
              >
                <X className="size-3" aria-hidden="true" />
              </button>
            ) : (
              <span
                aria-label="不可撤回（codex 后端）"
                title="codex turn/steer 协议一发即不可撤"
                className="inline-flex size-5 shrink-0 items-center justify-center text-subtle-foreground"
              >
                <Lock className="size-3" aria-hidden="true" />
              </span>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}
