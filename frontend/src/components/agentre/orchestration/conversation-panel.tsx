import * as React from "react";
import { useTranslation } from "react-i18next";
import { ChevronLeft, SendHorizontal, Clock } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { ChatTranscript } from "../chat";
import type { AgentColor } from "../types";
import { AgentAvatar, StatusDot } from "../primitives";
import { useOrchSubagentsStore } from "../../../stores/orch-subagents-store";
import { useOrchRunStore } from "../../../stores/orch-run-store";
import { RunSpeak } from "../../../../wailsjs/go/app/App";

const EMPTY_MESSAGES: never[] = [];

export function ConversationPanel({
  sessionId,
  agentName,
  agentColor,
  onBack,
  runId,
  agentId,
}: {
  sessionId: number;
  agentName: string;
  agentColor: AgentColor;
  onBack: () => void;
  /** Optional: run id used to look up activeAsks for the waiting callout */
  runId?: number;
  /** Optional: agent id used to look up activeAsks for the waiting callout */
  agentId?: number;
}) {
  const { t } = useTranslation();
  const ensureLoaded = useOrchSubagentsStore((s) => s.ensureLoaded);
  const reload = useOrchSubagentsStore((s) => s.reload);
  const messagesBySession = useOrchSubagentsStore((s) => s.messagesBySession);
  const messages = messagesBySession.get(sessionId) ?? EMPTY_MESSAGES;
  const [draft, setDraft] = React.useState("");
  const [sending, setSending] = React.useState(false);
  const [scrollEl, setScrollEl] = React.useState<HTMLDivElement | null>(null);

  // Derive awaiting state: is this agent currently waiting for a peer reply?
  const isAwaiting = useOrchRunStore((s) => {
    if (!runId || !agentId) return false;
    return (s.activeAsks.get(runId) ?? []).some(
      (ask) => ask.askerAgentId === agentId,
    );
  });

  React.useEffect(() => {
    if (sessionId) ensureLoaded(sessionId);
  }, [sessionId, ensureLoaded]);

  const speak = async () => {
    const text = draft.trim();
    if (!text || sending) return;
    setSending(true);
    try {
      await RunSpeak(sessionId, text);
      setDraft("");
      reload(sessionId);
    } finally {
      setSending(false);
    }
  };

  return (
    <div
      data-testid="conversation-panel"
      className="flex h-full flex-col bg-sidebar"
    >
      {/* cvHead: bg-card, padding [12,14], border-b, gap-2, vertical */}
      <div className="flex shrink-0 flex-col gap-2 border-b border-border bg-card px-3.5 py-3">
        {/* back row: chevron-left + label, gap=4px */}
        <button
          data-testid="conversation-back"
          className="flex items-center gap-1 text-muted-foreground transition-colors hover:text-foreground"
          onClick={onBack}
          type="button"
        >
          <ChevronLeft className="size-3 shrink-0" aria-hidden="true" />
          <span className="text-[11px]">
            {t("orchestration.conversation.backToBoard")}
          </span>
        </button>

        {/* who row: avatar + name column fill + status dot */}
        <div className="flex items-center gap-2" data-testid="conversation-who">
          <AgentAvatar
            name={agentName}
            color={agentColor}
            size="sm"
            className="size-[26px] shrink-0 rounded-full"
          />
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <span
              data-testid="conversation-who-name"
              className="truncate text-[13px] font-semibold leading-none text-foreground"
            >
              {agentName}
            </span>
          </div>
          <StatusDot
            status={isAwaiting ? "waiting" : "running"}
            size="xs"
            className="size-[7px] shrink-0"
          />
        </div>
      </div>

      {/* cvBody: bg-sidebar, padding 14, gap-3, vertical, overflow hidden */}
      <div
        ref={setScrollEl}
        className="flex min-h-0 flex-1 flex-col gap-3 overflow-hidden bg-sidebar p-3.5"
      >
        {/* waiting callout — shown only when agent is awaiting a peer reply */}
        {isAwaiting && (
          <div
            data-testid="conversation-awaiting-callout"
            className="flex shrink-0 items-center gap-1.5 rounded-lg border-l-[3px] border-status-waiting bg-status-waiting-bg p-2.5"
          >
            <Clock
              className="size-3 shrink-0 text-status-waiting"
              aria-hidden="true"
            />
            <span className="text-[11px] font-semibold text-status-waiting">
              {t("orchestration.conversation.awaitingReply")}
            </span>
          </div>
        )}

        {/* read-only transcript: omit live/onRerun/onEdit props */}
        <div className="min-h-0 flex-1 overflow-y-auto">
          <ChatTranscript
            agentName={agentName}
            agentColor={agentColor}
            sessionId={sessionId}
            messages={messages}
            scrollElement={scrollEl}
            virtualize
            active
          />
        </div>
      </div>

      {/* cvInput: bg-card, padding [10,12], border-t, items-center, gap-2 */}
      <div className="flex shrink-0 items-center gap-2 border-t border-border bg-card px-3 py-2.5">
        {/* textarea: bg-input-bg rounded-lg border px-2.5 py-2 */}
        <Textarea
          data-testid="conversation-speak-input"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder={t("orchestration.conversation.speakPlaceholder")}
          rows={1}
          className="min-h-0 flex-1 resize-none rounded-lg border bg-input-bg px-2.5 py-2 text-xs"
        />
        {/* send button: bg-primary rounded-lg w-[30px] h-[30px] */}
        <Button
          data-testid="conversation-speak-send"
          className="size-[30px] shrink-0 rounded-lg bg-primary p-0 text-primary-foreground hover:bg-primary/90"
          disabled={!draft.trim() || sending}
          onClick={() => void speak()}
          aria-label={t("orchestration.conversation.speakSend")}
        >
          <SendHorizontal className="size-3.5" aria-hidden="true" />
        </Button>
      </div>
    </div>
  );
}
