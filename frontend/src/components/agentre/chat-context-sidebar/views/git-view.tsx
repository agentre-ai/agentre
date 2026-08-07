import * as React from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

import type { TFunction } from "i18next";

import { deriveGitRows, type GitRow } from "../git-rows";

import type { GitChangesState, GitScope } from "./use-git-changes";
import { useOpenFile } from "./use-open-file";

type Props = {
  /** 会话工作目录；空串表示这个会话没有工作目录，直接出空态。 */
  cwd: string;
  remote: boolean;
  scope: GitScope;
  baseRef: string;
  state: GitChangesState;
  onRetry: () => void;
};

// 五类状态各有稳定的字母与配色；字母对读屏隐藏，另给一个文本标签（无障碍约定）。
const STATUS_META: Record<string, { letter: string; className: string }> = {
  modified: { letter: "M", className: "text-status-waiting" },
  added: { letter: "A", className: "text-status-running" },
  deleted: { letter: "D", className: "text-destructive" },
  renamed: { letter: "R", className: "text-primary" },
  untracked: { letter: "?", className: "text-muted-foreground" },
};

/**
 * statusLabel 逐个写死 key 而不是拼 `chatContext.git.status.${status}` —— 动态
 * key 会从 i18n 的静态 key 覆盖检查里溜掉，漏翻译不会被测出来。
 */
function statusLabel(t: TFunction, status: string): string {
  switch (status) {
    case "added":
      return t("chatContext.git.status.added");
    case "deleted":
      return t("chatContext.git.status.deleted");
    case "renamed":
      return t("chatContext.git.status.renamed");
    case "untracked":
      return t("chatContext.git.status.untracked");
    default:
      return t("chatContext.git.status.modified");
  }
}

/**
 * GitView 是「文件」页的「Git」模式内容区：当前档的变动文件扁平列表。
 *
 * 取数、两档与基线都在 useGitChanges 里，这里只负责把状态渲染成行与各种空态。
 * 行点击与「变动」模式一致：本地会话用系统默认应用打开，远端会话不提供打开。
 */
export function GitView({
  cwd,
  remote,
  scope,
  baseRef,
  state,
  onRetry,
}: Props) {
  const { t } = useTranslation();
  const openFile = useOpenFile(cwd);
  const canOpen = cwd !== "" && !remote;

  const changes = state.status === "loaded" ? state.view.changes : null;
  const rows = React.useMemo(() => deriveGitRows(changes), [changes]);

  if (cwd === "") return <Notice text={t("chatContext.git.noCwd")} />;

  if (state.status === "loading") {
    return <Skeleton label={t("chatContext.git.loading")} />;
  }

  if (state.status === "error") {
    return (
      <div className="px-3 py-6 text-center text-xs leading-relaxed text-muted-foreground">
        <p>{state.message || t("chatContext.git.readFailed")}</p>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="mt-2.5 h-7 text-[11px]"
          onClick={onRetry}
        >
          {t("chatContext.git.retry")}
        </Button>
      </div>
    );
  }

  if (state.view.notARepo) {
    return (
      <Notice
        text={t("chatContext.git.notARepo")}
        hint={t("chatContext.git.notARepoHint")}
      />
    );
  }

  // 本分支档拿不到基线：origin/HEAD / main / master 都不可得，引导用户自己选一个。
  if (scope === "branch" && baseRef === "") {
    return (
      <Notice
        text={t("chatContext.git.noBaseline")}
        hint={t("chatContext.git.noBaselineHint")}
      />
    );
  }

  if (rows.length === 0) {
    return (
      <Notice
        text={
          scope === "branch"
            ? t("chatContext.git.cleanBranch", { ref: baseRef })
            : t("chatContext.git.cleanUncommitted")
        }
      />
    );
  }

  return (
    <div className="flex flex-col gap-0.5 px-2 py-2.5">
      {rows.map((row) => (
        <Row
          key={row.path}
          row={row}
          onOpen={canOpen ? () => openFile(row.path) : null}
        />
      ))}
      {state.view.truncated ? (
        <div className="py-1.5 pr-2.5 pl-2 text-[11px] text-muted-foreground">
          {t("chatContext.git.truncated", { limit: rows.length })}
        </div>
      ) : null}
    </div>
  );
}

function Row({ row, onOpen }: { row: GitRow; onOpen: (() => void) | null }) {
  const { t } = useTranslation();
  const meta = STATUS_META[row.status] ?? STATUS_META.modified;
  const body = (
    <>
      <span
        data-status-letter
        aria-hidden="true"
        className={cn(
          "w-3 shrink-0 text-center font-mono text-[10px] font-bold",
          meta.className,
        )}
      >
        {meta.letter}
      </span>
      <span className="sr-only">{statusLabel(t, row.status)}</span>
      {/*
        窄栏下先挤掉的是目录后缀：它的 shrink 权重大到会先缩到没有，文件名因此
        实际上永不被截断（决策 11）；只有 basename 自己就超宽时才轮到它省略。
      */}
      <span className="min-w-0 shrink truncate font-mono">{row.name}</span>
      {/* 目录后缀从头截断（rtl 让省略号落在开头），根目录下的文件此处为空。 */}
      <span
        dir="rtl"
        className="min-w-0 flex-1 shrink-[9999] truncate text-left font-mono text-[10px] opacity-55"
      >
        {row.dir}
      </span>
      <DiffBadge row={row} />
    </>
  );
  const className =
    "flex w-full items-center gap-1.5 rounded-md py-1.5 pr-2.5 pl-2 text-left text-xs text-muted-foreground";
  const shared = {
    "data-testid": "git-row",
    "data-path": row.path,
    "data-status": row.status,
    title: row.oldPath ? `${row.oldPath} → ${row.path}` : row.path,
  };
  if (!onOpen) {
    return (
      <div {...shared} className={className}>
        {body}
      </div>
    );
  }
  return (
    <button
      type="button"
      {...shared}
      onClick={onOpen}
      className={cn(className, "transition-colors hover:bg-muted/50")}
    >
      {body}
    </button>
  );
}

/** 二进制文件没有可信的行数，只列出文件本身；两者皆为 0 时同样不出角标。 */
function DiffBadge({ row }: { row: GitRow }) {
  if (row.binary || (row.added <= 0 && row.deleted <= 0)) return null;
  return (
    <span
      aria-hidden="true"
      className="inline-flex shrink-0 items-center gap-1 font-mono text-[10px] font-medium"
    >
      {row.added > 0 ? (
        <span className="text-status-running">+{row.added}</span>
      ) : null}
      {row.deleted > 0 ? (
        <span className="text-destructive">−{row.deleted}</span>
      ) : null}
    </span>
  );
}

function Notice({ text, hint }: { text: string; hint?: string }) {
  return (
    <div className="px-3 py-6 text-center text-xs leading-relaxed text-muted-foreground">
      {text}
      {hint ? (
        <span className="mt-1.5 block text-[11px] opacity-80">{hint}</span>
      ) : null}
    </div>
  );
}

function Skeleton({ label }: { label: string }) {
  return (
    <div className="flex flex-col gap-2 px-3 py-3">
      <span role="status" className="sr-only">
        {label}
      </span>
      {[70, 52, 81, 44].map((width) => (
        <div
          key={width}
          className="h-2.5 animate-pulse rounded-sm bg-muted"
          style={{ width: `${width}%` }}
          aria-hidden="true"
        />
      ))}
    </div>
  );
}
