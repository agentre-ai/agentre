import { FileCode, Pencil } from "lucide-react";
import { useTranslation } from "react-i18next";

import type { FileEntry } from "../derive";

type Props = {
  files: FileEntry[];
  onJumpToTurn: (turn: number) => void;
};

function shortPath(p: string): string {
  const parts = p.split("/");
  return parts.length <= 2 ? p : parts.slice(-2).join("/");
}

export function FilesView({ files, onJumpToTurn }: Props) {
  const { t } = useTranslation();

  if (files.length === 0) {
    return (
      <div className="px-3 py-6 text-center text-xs text-muted-foreground">
        {t("chatContext.files.empty")}
      </div>
    );
  }
  return (
    <div className="flex flex-col gap-0.5 px-2 py-2.5">
      {files.map((f) => (
        <button
          key={f.path}
          type="button"
          onClick={() => onJumpToTurn(f.lastTurn)}
          className="flex items-center gap-2 rounded-md px-2.5 py-1.5 text-left text-xs text-muted-foreground transition-colors hover:bg-muted/50"
          title={f.path}
        >
          <FileCode
            className="size-3.5 shrink-0 text-muted-foreground"
            aria-hidden="true"
          />
          <span className="flex-1 truncate font-mono">{shortPath(f.path)}</span>
          {f.edits > 0 ? (
            <span className="inline-flex shrink-0 items-center gap-0.5 text-[10px] font-medium text-foreground">
              <Pencil className="size-2.5" aria-hidden="true" />
              {f.edits}
            </span>
          ) : null}
        </button>
      ))}
    </div>
  );
}
