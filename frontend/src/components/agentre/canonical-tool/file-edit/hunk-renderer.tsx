import { useTranslation } from "react-i18next";

import { cn } from "@/lib/utils";
import type { DiffHunk, DiffLine, FileEditPatch } from "../types";

// FileBlock 渲染单个文件的 diff(可能多 hunks);showHeader=true 时画出文件名条。
export function FileBlock({
  file,
  showHeader,
}: {
  file: FileEditPatch;
  showHeader: boolean;
}) {
  const { t } = useTranslation();
  const empty = !file.hunks || file.hunks.length === 0;
  return (
    <div>
      {showHeader && (
        <div className="flex items-center gap-2 border-y border-border bg-secondary px-3 py-1.5">
          <span className="font-mono text-[11px] font-semibold text-foreground">
            {file.path}
          </span>
          {file.kind === "created" && (
            <span className="rounded-sm bg-status-running-bg px-1.5 py-0.5 text-[9px] font-semibold tracking-[0.04em] text-status-running">
              {t("canonical.fileEdit.badge.new")}
            </span>
          )}
          {file.kind === "deleted" && (
            <span className="rounded-sm bg-destructive-soft px-1.5 py-0.5 text-[9px] font-semibold tracking-[0.04em] text-destructive">
              {t("canonical.fileEdit.badge.deleted")}
            </span>
          )}
          <span className="ml-auto font-mono text-[10px] font-semibold text-status-running">
            +{file.plus}
          </span>
          {file.minus > 0 && (
            <span className="font-mono text-[10px] font-semibold text-destructive">
              −{file.minus}
            </span>
          )}
        </div>
      )}
      {empty ? (
        <div className="px-3 py-2 text-muted-foreground">
          {t("canonical.fileEdit.noChanges")}
        </div>
      ) : (
        file.hunks.map((hunk, hi) => <HunkBlock key={hi} hunk={hunk} />)
      )}
      {file.truncated && (
        <div className="border-t border-border bg-secondary px-3 py-1 text-[11px] text-muted-foreground">
          {t("canonical.fileEdit.truncated", {
            count: file.plus + file.minus,
            shown: 200,
          })}
        </div>
      )}
    </div>
  );
}

function HunkBlock({ hunk }: { hunk: DiffHunk }) {
  return (
    <>
      <div className="bg-secondary px-3 py-1 font-mono text-[11px] font-semibold text-muted-foreground">
        @@ -{hunk.oldStart},{hunk.oldLines} +{hunk.newStart},{hunk.newLines} @@
        {hunk.header ? (
          <span className="ml-3 font-normal text-subtle-foreground">
            {hunk.header}
          </span>
        ) : null}
      </div>
      {hunk.lines.map((l, li) => (
        <DiffLineRow key={li} line={l} />
      ))}
    </>
  );
}

function DiffLineRow({ line }: { line: DiffLine }) {
  const bg =
    line.op === "+"
      ? "bg-status-running-bg"
      : line.op === "-"
        ? "bg-destructive-soft"
        : "";
  const markColor =
    line.op === "+"
      ? "text-status-running"
      : line.op === "-"
        ? "text-destructive"
        : "text-subtle-foreground";
  return (
    <div className={cn("flex items-center px-3 py-0.5", bg)}>
      <span className="w-8 text-right text-[11px] text-subtle-foreground">
        {line.old ?? " "}
      </span>
      <span className="w-8 text-right text-[11px] text-subtle-foreground">
        {line.new ?? " "}
      </span>
      <span
        className={cn("w-5 text-center text-[11px] font-semibold", markColor)}
      >
        {line.op}
      </span>
      <span className="whitespace-pre text-foreground">{line.text}</span>
    </div>
  );
}
