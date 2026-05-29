import * as React from "react";
import { useTranslation } from "react-i18next";
import {
  CheckCircle2,
  ClipboardList,
  MessageSquareText,
  Send,
  X,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { useChatStreamsStore } from "@/stores/chat-streams-store";
import { ResolvePlanAction as wailsResolvePlanAction } from "../../../../../wailsjs/go/app/App";

import { MarkdownText } from "../../markdown-text";
import type { CanonicalCardProps, PlanActionStream } from "../props";
import type { CanonicalDTO, PlanActionDTO } from "../types";

import { metaFor, splitActions } from "./action-labels";

type NormalizedPlan = {
  allowed?: boolean;
  requestId: string;
  resolved?: boolean;
  text: string;
  actions: PlanActionDTO[];
};

function readPlan(toolBlock: unknown): NormalizedPlan | undefined {
  const block = toolBlock as {
    canonical?: CanonicalDTO;
    text?: string;
    type?: string;
  };
  const canonical = block.canonical;
  if (canonical?.kind === "plan.approve_request") {
    const plan = canonical.planApprove;
    if (!plan.requestId) return undefined;
    return {
      allowed: plan.allowed,
      requestId: plan.requestId,
      resolved: plan.resolved,
      text: plan.planText ?? "",
      actions: plan.actions ?? [],
    };
  }
  if (canonical?.kind === "plan.update" && block.type === "plan") {
    const plan = canonical.planUpdate;
    const text = plan.text ?? block.text ?? "";
    const actions = plan.actions ?? [];
    if (!text && actions.length === 0) return undefined;
    return {
      requestId: "",
      text,
      actions,
    };
  }
  return undefined;
}

function errorMessage(err: unknown): string {
  if (err instanceof Error && err.message) return err.message;
  if (typeof err === "string" && err.trim()) return err;
  if (err && typeof err === "object") {
    for (const key of ["message", "error", "detail", "details", "reason"]) {
      const value = (err as Record<string, unknown>)[key];
      if (typeof value === "string" && value.trim()) return value;
      if (value instanceof Error && value.message) return value.message;
    }
    const text = String(err);
    if (text && text !== "[object Object]") return text;
  }
  try {
    const text = JSON.stringify(err);
    if (text && text !== "{}") return text;
  } catch {
    // fall through to generic text
  }
  return "Submit failed";
}

function readPlanActionStream(resp: unknown): PlanActionStream | null {
  if (!resp || typeof resp !== "object") return null;
  const value = resp as Partial<PlanActionStream>;
  if (
    typeof value.stream !== "string" ||
    value.stream.length === 0 ||
    typeof value.sessionId !== "number" ||
    value.sessionId <= 0 ||
    typeof value.userMessageId !== "number" ||
    value.userMessageId <= 0 ||
    typeof value.assistantMessageId !== "number" ||
    value.assistantMessageId <= 0
  ) {
    return null;
  }
  return {
    sessionId: value.sessionId,
    userMessageId: value.userMessageId,
    assistantMessageId: value.assistantMessageId,
    stream: value.stream,
  };
}

function sendTextForAction(
  action: PlanActionDTO,
  requestId: string,
  feedbackText: string,
  t: (key: string) => string,
): string {
  if (action.id === "plan.execute") return "Implement the plan.";
  if (action.id === "plan.refine" && !requestId) {
    return feedbackText.trim() || t("canonical.plan.refineDefaultPrompt");
  }
  return "";
}

export const PlanCard: React.FC<CanonicalCardProps> = ({
  toolBlock,
  sessionId,
  onPlanActionStarted,
}) => {
  const { t } = useTranslation();
  const plan = readPlan(toolBlock);
  const streamActive = useChatStreamsStore((s) =>
    sessionId ? s.streams.has(sessionId) : false,
  );

  const [expanded, setExpanded] = React.useState(() => !!plan?.text);
  const [feedbackOpen, setFeedbackOpen] = React.useState(false);
  const [feedback, setFeedback] = React.useState("");
  const [submitting, setSubmitting] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [localResolution, setLocalResolution] = React.useState<{
    allowed: boolean;
    resolved: boolean;
  } | null>(null);

  if (!plan) return null;
  const activePlan = plan;

  const bodyText = activePlan.text.replace(/^#?\s*Plan\s*\n?/i, "");
  const resolved = !!activePlan.resolved || !!localResolution?.resolved;
  const allowed = activePlan.resolved
    ? activePlan.allowed
    : localResolution?.allowed;
  const approved = resolved && !!allowed;
  const visibleActions: PlanActionDTO[] = resolved ? [] : activePlan.actions;
  const { primary: primaryActions, refine: refineAction } =
    splitActions(visibleActions);
  const startsNewTurn = !activePlan.requestId;
  const actionsDisabled = (streamActive && startsNewTurn) || submitting;

  async function dispatchAction(action: PlanActionDTO, feedbackText: string) {
    if (!sessionId) return;
    if (submitting) return;
    if (streamActive && startsNewTurn) return;
    setError(null);
    setSubmitting(true);
    try {
      const resp = (await wailsResolvePlanAction({
        sessionId,
        requestId: activePlan.requestId,
        actionId: action.id,
        feedback: feedbackText,
      } as Parameters<typeof wailsResolvePlanAction>[0])) as unknown;
      const userText = sendTextForAction(
        action,
        activePlan.requestId,
        feedbackText,
        t,
      );
      const stream = userText ? readPlanActionStream(resp) : null;
      if (stream) {
        onPlanActionStarted?.(stream, userText);
      }
      if (!activePlan.requestId) {
        setLocalResolution({
          resolved: true,
          allowed: action.kind !== "refine",
        });
      }
      if (action.kind === "refine") {
        setFeedbackOpen(false);
        setFeedback("");
      }
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setSubmitting(false);
    }
  }

  const title = resolved
    ? approved
      ? t("canonical.plan.title.approved")
      : t("canonical.plan.title.refining")
    : t("canonical.plan.title.pending");
  const subtitle = resolved
    ? approved
      ? t("canonical.plan.subtitle.approved")
      : t("canonical.plan.subtitle.refining")
    : visibleActions.length > 0
      ? t("canonical.plan.subtitle.chooseAction")
      : t("canonical.plan.subtitle.saved");

  return (
    <section
      data-testid="plan-card"
      role="region"
      aria-label={t("canonical.plan.aria")}
      className={cn(
        "w-full max-w-[720px] overflow-hidden rounded-md border bg-card",
        resolved
          ? approved
            ? "border-status-running/50"
            : "border-border"
          : "border-primary",
      )}
    >
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        aria-expanded={expanded}
        className={cn(
          "flex w-full min-w-0 items-center gap-3 px-3 py-2 text-left transition-colors",
          resolved
            ? "hover:bg-muted/40"
            : "bg-status-waiting-bg/40 hover:bg-status-waiting-bg/60",
        )}
      >
        <span
          className={cn(
            "flex size-7 shrink-0 items-center justify-center rounded-md",
            approved
              ? "bg-status-running-bg text-status-running"
              : "bg-primary text-primary-foreground",
          )}
        >
          {approved ? (
            <CheckCircle2 className="size-4" aria-hidden />
          ) : (
            <ClipboardList className="size-4" aria-hidden />
          )}
        </span>
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-semibold text-foreground">
            {title}
          </div>
          <div className="truncate text-xs text-muted-foreground">
            {subtitle}
          </div>
        </div>
        {activePlan.text ? (
          <span className="rounded-sm border border-border bg-card px-2 py-0.5 font-mono text-[10px] text-muted-foreground">
            {t("canonical.plan.charCount", {
              count: activePlan.text.length,
            })}
          </span>
        ) : null}
      </button>

      {expanded ? (
        <div className="max-h-[480px] overflow-auto px-4 py-3">
          {bodyText ? (
            <MarkdownText text={bodyText} />
          ) : (
            <div className="text-xs text-muted-foreground">
              {t("canonical.plan.empty")}
            </div>
          )}
        </div>
      ) : null}

      {visibleActions.length > 0 ? (
        <div className="flex flex-wrap items-center gap-2 border-t border-border px-3 py-2.5">
          {primaryActions.map((action) => {
            const meta = metaFor(action, activePlan, t);
            const Icon = meta.icon;
            return (
              <Button
                key={action.id}
                size="sm"
                variant={meta.variant}
                disabled={actionsDisabled}
                onClick={() => void dispatchAction(action, "")}
              >
                <Icon className="mr-1 size-3.5" aria-hidden />
                {meta.label}
              </Button>
            );
          })}
          {refineAction ? (
            <Button
              size="sm"
              variant={metaFor(refineAction, activePlan, t).variant}
              disabled={actionsDisabled}
              onClick={() => setFeedbackOpen((v) => !v)}
              aria-expanded={feedbackOpen}
            >
              {feedbackOpen ? (
                <X className="mr-1 size-3.5" aria-hidden />
              ) : (
                (() => {
                  const Icon = metaFor(refineAction, activePlan, t).icon;
                  return <Icon className="mr-1 size-3.5" aria-hidden />;
                })()
              )}
              {feedbackOpen
                ? t("canonical.plan.feedback.collapse")
                : metaFor(refineAction, activePlan, t).label}
            </Button>
          ) : null}
          {error ? (
            <span className="text-xs text-destructive">{error}</span>
          ) : null}
        </div>
      ) : null}

      {feedbackOpen && refineAction ? (
        <div className="flex flex-col gap-2 border-t border-border bg-muted/50 px-4 py-3">
          <div className="flex items-center gap-1.5 text-xs text-foreground">
            <MessageSquareText className="size-3.5 text-muted-foreground" />
            <span className="font-medium">
              {t("canonical.plan.feedback.title")}
            </span>
            <span className="text-muted-foreground">
              {t("canonical.plan.feedback.description")}
            </span>
          </div>
          <Textarea
            value={feedback}
            onChange={(e) => setFeedback(e.target.value)}
            placeholder={t("canonical.plan.feedback.placeholder")}
            rows={3}
            disabled={actionsDisabled}
            className="text-sm"
          />
          <div className="flex items-center justify-between gap-2">
            <span className="font-mono text-[10px] text-subtle-foreground">
              {t("canonical.plan.feedback.charCount", {
                count: feedback.length,
              })}
            </span>
            <Button
              size="sm"
              disabled={actionsDisabled}
              onClick={() => void dispatchAction(refineAction, feedback.trim())}
            >
              <Send className="mr-1 size-3.5" aria-hidden />
              {feedback.trim()
                ? t("canonical.plan.feedback.sendWithFeedback")
                : t("canonical.plan.feedback.sendWithoutFeedback")}
            </Button>
          </div>
        </div>
      ) : null}
    </section>
  );
};
