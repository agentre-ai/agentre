import * as React from "react";
import { useTranslation } from "react-i18next";
import { ArrowLeft, SendHorizontal } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { ChatTranscript } from "../chat";
import type { AgentColor } from "../types";
import { useOrchSubagentsStore } from "../../../stores/orch-subagents-store";
import { RunSpeak } from "../../../../wailsjs/go/app/App";

const EMPTY_MESSAGES: never[] = [];

export function ConversationPanel({
  sessionId,
  agentName,
  agentColor,
  onBack,
}: {
  sessionId: number;
  agentName: string;
  agentColor: AgentColor;
  onBack: () => void;
}) {
  const { t } = useTranslation();
  const ensureLoaded = useOrchSubagentsStore((s) => s.ensureLoaded);
  const messagesBySession = useOrchSubagentsStore((s) => s.messagesBySession);
  const messages = messagesBySession.get(sessionId) ?? EMPTY_MESSAGES;
  const [draft, setDraft] = React.useState("");
  const [sending, setSending] = React.useState(false);
  const scrollRef = React.useRef<HTMLDivElement | null>(null);

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
    } finally {
      setSending(false);
    }
  };

  return (
    <div data-testid="conversation-panel" className="flex h-full flex-col">
      {/* header: back button + agent name */}
      <div className="flex shrink-0 items-center gap-2 border-b border-border p-2">
        <Button
          data-testid="conversation-back"
          variant="ghost"
          size="sm"
          className="h-7 px-2 text-xs"
          onClick={onBack}
        >
          <ArrowLeft className="size-3.5" />
          {t("orchestration.conversation.backToBoard")}
        </Button>
        <span className="truncate text-sm font-medium text-foreground">
          {agentName}
        </span>
      </div>

      {/* read-only transcript: omit live/onRerun/onEdit props */}
      <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto">
        <ChatTranscript
          agentName={agentName}
          agentColor={agentColor}
          sessionId={sessionId}
          messages={messages}
          scrollElement={scrollRef.current}
          virtualize
          active
        />
      </div>

      {/* speak input */}
      <div className="flex shrink-0 items-end gap-2 border-t border-border p-2">
        <Textarea
          data-testid="conversation-speak-input"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder={t("orchestration.conversation.speakPlaceholder")}
          rows={1}
          className="min-h-8 resize-none text-xs"
        />
        <Button
          data-testid="conversation-speak-send"
          size="sm"
          className="h-8 shrink-0 px-2"
          disabled={!draft.trim() || sending}
          onClick={() => void speak()}
          aria-label={t("orchestration.conversation.speakSend")}
        >
          <SendHorizontal className="size-3.5" />
        </Button>
      </div>
    </div>
  );
}
