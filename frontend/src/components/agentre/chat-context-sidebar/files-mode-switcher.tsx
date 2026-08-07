import { useTranslation } from "react-i18next";

import { cn } from "@/lib/utils";

import type { ChatFilesMode } from "@/stores/chat-sidebar-store";

type Props = {
  active: ChatFilesMode;
  onChange: (mode: ChatFilesMode) => void;
  /** 「变动」段的角标恒显示 deriveFiles() 的文件数（零成本）。 */
  changesCount: number;
  /** 「Git」段的角标：数据已加载时才有值，未加载 / 加载中传 null。 */
  gitCount?: number | null;
};

/**
 * FilesModeSwitcher 是「文件」页内的三段控件（变动 / 目录 / Git）。三种模式是
 * 同一批文件的三种视角，与顶层的「大纲 / 文件」不同级，所以下沉到页内而不是
 * 提到顶层 tab（设计决策 1）。
 *
 * 各段等宽且 whitespace-nowrap，侧栏被拖窄到 190px 时不换行。
 */
export function FilesModeSwitcher({
  active,
  onChange,
  changesCount,
  gitCount = null,
}: Props) {
  const { t } = useTranslation();
  return (
    <div
      role="tablist"
      aria-label={t("chatContext.files.modes.label")}
      className="flex h-8 shrink-0 items-center gap-0.5 border-b border-border px-2"
    >
      <ModeTab
        label={t("chatContext.files.modes.changes")}
        count={changesCount}
        active={active === "changes"}
        onClick={() => onChange("changes")}
      />
      <ModeTab
        label={t("chatContext.files.modes.directory")}
        count={null}
        active={active === "directory"}
        onClick={() => onChange("directory")}
      />
      <ModeTab
        label={t("chatContext.files.modes.git")}
        count={gitCount}
        active={active === "git"}
        onClick={() => onChange("git")}
      />
    </div>
  );
}

function ModeTab({
  label,
  count,
  active,
  onClick,
}: {
  label: string;
  count: number | null;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={cn(
        "inline-flex min-w-0 flex-1 items-center justify-center gap-1 rounded-md px-0.5 py-1 text-[11px] whitespace-nowrap transition-colors",
        "focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none",
        active
          ? "bg-accent font-semibold text-foreground"
          : "text-muted-foreground hover:bg-muted/50",
      )}
    >
      <span className="truncate">{label}</span>
      {count != null ? (
        <span className="shrink-0 font-mono text-[10px] opacity-75">
          {count}
        </span>
      ) : null}
    </button>
  );
}
