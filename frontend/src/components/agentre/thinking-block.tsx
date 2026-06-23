import * as React from "react";
import { useTranslation } from "react-i18next";
import { Brain, ChevronDown } from "lucide-react";

import { cn } from "@/lib/utils";

import { shouldIgnoreClickForSelection } from "./copyable-text";
import { useTranscriptBooleanState } from "./transcript-ui-state";

type ThinkingBlockProps = {
  text: string;
  /** 该 block 是否正处在流式输出中。父组件根据 stream 上下文判断后传入。 */
  streaming: boolean;
  /**
   * 外部传入的计时起点 (Unix ms)。父组件已知更早的起点(例如 stream 真正开始的瞬间) 时传入,
   * 用于解决 Claude Code CLI 把整段 thinking 一次性发出来、合成块只活几 ms 自计时只能拿到 0s 的问题。
   * 不传时退化为「组件首次挂载时」自计时。
   */
  startedAt?: number;
  uiStateKey?: string;
};

export function ThinkingBlock({
  text,
  streaming,
  startedAt: externalStartedAt,
  uiStateKey,
}: ThinkingBlockProps) {
  const { t } = useTranslation();
  // streaming 期间默认展开,纯历史(streaming=false 渲染)默认折叠,完成时再强制收回(见下方 effect)。
  const [expanded, setExpanded] = useTranscriptBooleanState(
    uiStateKey,
    streaming,
  );
  // 自计时回退:仅当外部没传 startedAt 时使用。
  const [internalStartedAt, setInternalStartedAt] = React.useState<
    number | null
  >(null);
  const startedAt = externalStartedAt ?? internalStartedAt;
  const [liveSeconds, setLiveSeconds] = React.useState(0);
  const [finalSeconds, setFinalSeconds] = React.useState<number | null>(null);
  // streaming body 在内容超过 max-h 时手动把 scrollTop 推到底部，
  // 短内容下 scrollHeight === clientHeight，scrollTop 自然为 0，没有空白。
  const streamingBodyRef = React.useRef<HTMLDivElement>(null);
  React.useLayoutEffect(() => {
    if (!streaming) return;
    const el = streamingBodyRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [streaming, text]);

  // streaming → done 转换瞬间强制收回展开态,避免用户在 streaming 中点开后完成态仍卡在展开。
  const prevStreamingRef = React.useRef(streaming);
  React.useEffect(() => {
    if (prevStreamingRef.current && !streaming) {
      setExpanded(false);
    }
    prevStreamingRef.current = streaming;
  }, [setExpanded, streaming]);

  React.useEffect(() => {
    if (!streaming) {
      // 流式结束:如果曾有起点 (外部传入或自记),把当前 (Date.now() - startedAt) 固化为 final。
      if (startedAt !== null && finalSeconds === null) {
        setFinalSeconds(Math.floor((Date.now() - startedAt) / 1000));
      }
      return undefined;
    }
    // 流式中:外部没传 startedAt 时首次进入记 internalStartedAt;然后每秒推进 liveSeconds。
    if (externalStartedAt === undefined && internalStartedAt === null) {
      setInternalStartedAt(Date.now());
    }
    const start = startedAt ?? Date.now();
    setLiveSeconds(Math.floor((Date.now() - start) / 1000));
    const id = setInterval(() => {
      setLiveSeconds(Math.floor((Date.now() - start) / 1000));
    }, 1000);
    return () => clearInterval(id);
  }, [
    streaming,
    startedAt,
    finalSeconds,
    externalStartedAt,
    internalStartedAt,
  ]);

  if (!text) return null;

  const charCount = text.length;

  let metaText = "";
  if (!streaming) {
    metaText =
      finalSeconds !== null
        ? t("thinking.meta.withSeconds", {
            seconds: finalSeconds,
            count: charCount,
          })
        : t("thinking.meta.charCount", { count: charCount });
  }

  const handleToggle = (event: React.MouseEvent<HTMLButtonElement>) => {
    if (shouldIgnoreClickForSelection(event)) return;
    setExpanded((v) => !v);
  };

  return (
    <div
      data-selectable-text="true"
      className="overflow-hidden rounded-lg border border-border bg-card"
    >
      <button
        type="button"
        onClick={handleToggle}
        aria-expanded={expanded}
        aria-label={
          streaming ? t("thinking.toggleStreaming") : t("thinking.toggleDone")
        }
        className="flex w-full cursor-pointer items-center gap-2 px-3.5 py-2.5 text-left hover:bg-muted/40"
      >
        <Brain
          aria-hidden
          className={cn(
            "size-4 shrink-0 text-primary",
            !streaming && "opacity-70",
          )}
        />
        <span
          data-copyable-control-text="true"
          className="text-sm font-medium text-foreground"
        >
          {streaming ? t("thinking.streaming") : t("thinking.done")}
        </span>
        {streaming ? (
          <span
            data-copyable-control-text="true"
            className="rounded bg-primary/10 px-1.5 py-0.5 font-mono text-2xs font-medium text-primary-text"
          >
            {liveSeconds}s
          </span>
        ) : metaText ? (
          <span
            data-copyable-control-text="true"
            className="text-xs text-muted-foreground"
          >
            {metaText}
          </span>
        ) : null}
        <span className="flex-1" />
        {streaming ? (
          <span
            aria-hidden
            className="size-1.5 shrink-0 rounded-full bg-primary motion-safe:animate-pulse"
          />
        ) : null}
        <ChevronDown
          aria-hidden
          className={cn(
            "size-4 shrink-0 text-muted-foreground transition-transform duration-150 ease-out motion-reduce:transition-none",
            expanded && "rotate-180",
          )}
        />
      </button>
      <div
        data-slot="thinking-block-content"
        className="grid transition-[grid-template-rows] duration-200 ease-out motion-reduce:transition-none"
        style={{ gridTemplateRows: expanded ? "1fr" : "0fr" }}
        aria-hidden={!expanded}
      >
        <div className="min-h-0 overflow-hidden">
          <div className="border-t border-border">
            {streaming ? (
              <div
                ref={streamingBodyRef}
                className="max-h-[132px] overflow-hidden whitespace-pre-wrap break-words px-3.5 py-3 text-xs italic leading-[1.55] text-muted-foreground"
              >
                {text}
              </div>
            ) : (
              <div className="whitespace-pre-wrap break-words px-3.5 py-3 text-xs italic leading-[1.55] text-muted-foreground">
                {text}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
