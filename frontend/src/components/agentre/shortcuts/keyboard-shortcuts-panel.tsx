import React from "react";
import { TriangleAlert } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { cn } from "@/lib/utils";

import { chordFromEvent, formatChord } from "./format";
import { REGISTRY, TAB_CHIP_IDS, TAB_CLOSE_ID, getDef } from "./registry";
import { useShortcutsContext } from "./shortcuts-provider";
import type { KeyChord, ShortcutDef } from "./types";

const CONFLICT_TOAST_MS = 4000;

type ConflictMessage = {
  text: string;
  highlightId?: string;
};

function ShortcutRow({
  def,
  chord,
  recording,
  highlighted,
  fixed,
  onStartRecord,
  onCancelRecord,
}: {
  def: ShortcutDef;
  chord: KeyChord | null;
  recording: boolean;
  highlighted: boolean;
  fixed?: boolean;
  onStartRecord: () => void;
  onCancelRecord: () => void;
}) {
  const { platform } = useShortcutsContext();
  return (
    <div
      data-slot="shortcut-row"
      className={cn(
        "flex items-center justify-between gap-4 rounded-lg border border-border bg-card px-4 py-3 transition-colors",
        recording && "bg-primary-soft/40",
        highlighted && "border-destructive bg-destructive/10",
      )}
    >
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="text-sm font-medium text-foreground">{def.label}</span>
        <span className="text-xs text-muted-foreground">{def.hint}</span>
      </div>
      <div className="flex items-center gap-3">
        <span
          className={cn(
            "inline-flex h-7 min-w-[64px] items-center justify-center rounded-md border border-border bg-muted px-2.5 font-mono text-2xs font-semibold tracking-wide text-foreground",
            recording &&
              "border-dashed border-primary/60 bg-background text-primary",
          )}
        >
          {recording
            ? "按下新组合键…"
            : chord
              ? formatChord(chord, platform)
              : "未绑定"}
        </span>
        {fixed ? (
          <span className="rounded-md bg-muted px-2 py-1 text-2xs font-semibold uppercase tracking-wide text-muted-foreground">
            固定
          </span>
        ) : recording ? (
          <Button
            type="button"
            variant="destructive"
            size="sm"
            onClick={onCancelRecord}
          >
            取消
          </Button>
        ) : (
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={onStartRecord}
          >
            录制
          </Button>
        )}
      </div>
    </div>
  );
}

function ConflictBanner({ message }: { message: ConflictMessage }) {
  return (
    <div
      role="alert"
      className="flex items-center gap-3 rounded-md border border-destructive bg-destructive/10 px-4 py-3 text-sm text-destructive shadow-sm"
    >
      <TriangleAlert className="size-4 shrink-0" aria-hidden="true" />
      <span className="font-medium">{message.text}</span>
    </div>
  );
}

export function KeyboardShortcutsPanel(): React.ReactElement {
  const {
    bindings,
    platform,
    setBinding,
    setPaused,
    resetAll,
    findChordConflict,
  } = useShortcutsContext();

  const [recordingId, setRecordingId] = React.useState<string | null>(null);
  const [conflict, setConflict] = React.useState<ConflictMessage | null>(null);
  const [resetOpen, setResetOpen] = React.useState(false);

  const conflictTimerRef = React.useRef<number | null>(null);
  const showConflict = React.useCallback((msg: ConflictMessage) => {
    setConflict(msg);
    if (conflictTimerRef.current !== null) {
      window.clearTimeout(conflictTimerRef.current);
    }
    conflictTimerRef.current = window.setTimeout(
      () => setConflict(null),
      CONFLICT_TOAST_MS,
    );
  }, []);

  React.useEffect(() => {
    return () => {
      if (conflictTimerRef.current !== null) {
        window.clearTimeout(conflictTimerRef.current);
      }
    };
  }, []);

  // Pause the global state machine while recording so chords don't navigate.
  React.useEffect(() => {
    setPaused(recordingId !== null);
    return () => setPaused(false);
  }, [recordingId, setPaused]);

  // Listen for the new chord while recording.
  React.useEffect(() => {
    if (!recordingId) return;
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {
        event.preventDefault();
        setRecordingId(null);
        return;
      }
      const chord = chordFromEvent(event, platform);
      if (!chord) return;
      event.preventDefault();

      const conflictResult = findChordConflict(chord, recordingId!);
      if (conflictResult?.type === "system") {
        showConflict({ text: "该组合键由系统保留,请换一个" });
        return;
      }
      if (conflictResult?.type === "binding") {
        const other = getDef(conflictResult.id);
        showConflict({
          text: `已被「${other?.label ?? conflictResult.id}」占用`,
          highlightId: conflictResult.id,
        });
        return;
      }
      setBinding(recordingId!, chord);
      setRecordingId(null);
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [recordingId, platform, findChordConflict, setBinding, showConflict]);

  const globalDefs = REGISTRY.filter((def) => def.scope === "global");
  const chatTabDef = getDef(TAB_CHIP_IDS[0]!)!;
  const tabCloseDef = getDef(TAB_CLOSE_ID)!;

  const performReset = () => {
    resetAll();
    setRecordingId(null);
    setConflict(null);
    setResetOpen(false);
  };

  return (
    <>
      <div className="flex max-w-3xl flex-col gap-1.5">
        <h1 className="text-2xl font-semibold tracking-normal">键盘快捷键</h1>
        <p className="text-sm leading-relaxed text-muted-foreground">
          调整全局导航与对话页快捷键。
        </p>
      </div>

      <section className="flex flex-col gap-3">
        <h2 className="text-2xs font-semibold uppercase tracking-[0.08em] text-subtle-foreground">
          全局导航
        </h2>
        {globalDefs.map((def) => (
          <ShortcutRow
            key={def.id}
            def={def}
            chord={bindings.get(def.id) ?? null}
            recording={recordingId === def.id}
            highlighted={conflict?.highlightId === def.id}
            onStartRecord={() => {
              setRecordingId(def.id);
              setConflict(null);
            }}
            onCancelRecord={() => setRecordingId(null)}
          />
        ))}
      </section>

      <section className="flex flex-col gap-3">
        <h2 className="text-2xs font-semibold uppercase tracking-[0.08em] text-subtle-foreground">
          对话页
        </h2>
        <ShortcutRow
          fixed
          def={{
            ...chatTabDef,
            label: "切换到第 N 个 Tab",
            hint: `${formatChord(
              { mod: "primary", key: "1" },
              platform,
            )} - ${formatChord(
              { mod: "primary", key: "9" },
              platform,
            )} · 按 TabStrip 排列顺序（固定 + 普通 + 预览）`,
          }}
          chord={bindings.get(chatTabDef.id) ?? null}
          recording={false}
          highlighted={false}
          onStartRecord={() => {}}
          onCancelRecord={() => {}}
        />
        <ShortcutRow
          fixed
          def={tabCloseDef}
          chord={bindings.get(tabCloseDef.id) ?? null}
          recording={false}
          highlighted={false}
          onStartRecord={() => {}}
          onCancelRecord={() => {}}
        />
      </section>

      <div className="flex items-center justify-end gap-3">
        {conflict ? <ConflictBanner message={conflict} /> : null}
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => setResetOpen(true)}
        >
          全部重置为默认
        </Button>
      </div>

      <Dialog open={resetOpen} onOpenChange={setResetOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>重置所有快捷键?</DialogTitle>
            <DialogDescription>
              所有自定义绑定将恢复为默认值,无法撤销。
            </DialogDescription>
          </DialogHeader>
          <DialogBody>
            <p className="text-sm text-muted-foreground">
              重置后浏览器本地保存的
              <code className="mx-1 rounded bg-muted px-1.5 py-0.5 font-mono text-2xs">
                agentre.shortcuts
              </code>
              将被清空。
            </p>
          </DialogBody>
          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => setResetOpen(false)}
            >
              取消
            </Button>
            <Button
              type="button"
              variant="destructive"
              size="sm"
              onClick={performReset}
            >
              重置
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
