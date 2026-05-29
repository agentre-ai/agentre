import * as React from "react";
import { useTranslation } from "react-i18next";
import {
  AlertTriangle,
  ChevronRight,
  File as FileIcon,
  Folder as FolderIcon,
  Link as LinkIcon,
  Loader2,
  Plus,
  X,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

import { RemoteFsListDir, RemoteFsMkdir } from "../../../wailsjs/go/app/App";

// 与 wailsjs codegen 类型一致;codegen 跑后可改回 import remote_fs_svc.EntryView。
type EntryView = {
  name: string;
  isDir: boolean;
  size: number;
  mtime: number;
  symlink?: boolean;
};

type ListDirView = {
  path: string;
  entries: EntryView[];
  truncated: boolean;
};

export type RemoteFsPickerProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  deviceID: string;
  deviceName: string;
  mode: "dir"; // 留口子,未来加 'file'
  initialPath?: string;
  onPick: (path: string) => void;
};

const INVALID_NAME_RE = /[/]|^\s|\s$|^\.\.?$/;

export function RemoteFsPicker({
  open,
  onOpenChange,
  deviceID,
  deviceName,
  initialPath,
  onPick,
}: RemoteFsPickerProps) {
  const { t } = useTranslation();
  const [currentPath, setCurrentPath] = React.useState<string>("");
  const [entries, setEntries] = React.useState<EntryView[]>([]);
  const [truncated, setTruncated] = React.useState(false);
  const [filter, setFilter] = React.useState("");
  const [showHidden, setShowHidden] = React.useState(false);
  const [loading, setLoading] = React.useState(false);
  const [err, setErr] = React.useState<string | null>(null);
  const [creating, setCreating] = React.useState(false);
  const [newName, setNewName] = React.useState("");
  const [newErr, setNewErr] = React.useState<string | null>(null);
  const [selectedName, setSelectedName] = React.useState<string | null>(null);

  const load = React.useCallback(
    async (path: string) => {
      setLoading(true);
      setErr(null);
      try {
        const resp = (await RemoteFsListDir(deviceID, path)) as ListDirView;
        setCurrentPath(resp.path);
        setEntries(resp.entries ?? []);
        setTruncated(!!resp.truncated);
        setFilter("");
        setSelectedName(null);
      } catch (e) {
        setErr(String(e));
        setEntries([]);
        setTruncated(false);
      } finally {
        setLoading(false);
      }
    },
    [deviceID],
  );

  React.useEffect(() => {
    if (open) {
      void load(initialPath ?? "");
    } else {
      setCreating(false);
      setNewName("");
      setNewErr(null);
    }
  }, [open, initialPath, load]);

  const segments = React.useMemo(() => {
    const parts = currentPath.split("/").filter(Boolean);
    return parts.map((seg, i) => ({
      label: seg,
      path: "/" + parts.slice(0, i + 1).join("/"),
    }));
  }, [currentPath]);

  const filtered = React.useMemo(() => {
    const lower = filter.toLowerCase();
    return entries
      .filter((e) => (showHidden ? true : !e.name.startsWith(".")))
      .filter((e) => (lower ? e.name.toLowerCase().includes(lower) : true))
      .sort((a, b) => {
        if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
        return a.name.localeCompare(b.name, undefined, { numeric: true });
      });
  }, [entries, filter, showHidden]);

  const handleMkdir = async () => {
    setNewErr(null);
    const name = newName.trim();
    if (!name || INVALID_NAME_RE.test(name) || name.length > 255) {
      setNewErr(t("remoteFs.errors.invalidName"));
      return;
    }
    try {
      await RemoteFsMkdir(deviceID, currentPath, name);
      setCreating(false);
      setNewName("");
      await load(currentPath);
      setSelectedName(name);
    } catch (e) {
      setNewErr(String(e));
    }
  };

  const finalPath =
    selectedName != null ? joinPath(currentPath, selectedName) : currentPath;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="max-w-[560px]"
        showCloseButton
        aria-describedby={undefined}
      >
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-sm">
            {t("remoteFs.title")}
            <span className="text-2xs text-muted-foreground">
              · {deviceName}
            </span>
          </DialogTitle>
        </DialogHeader>

        <DialogBody className="flex max-h-[60vh] flex-col gap-2">
          {/* 面包屑 */}
          <div className="flex flex-wrap items-center gap-0.5 text-2xs">
            <BreadcrumbBtn label="/" onClick={() => void load("/")} />
            {segments.map((s) => (
              <React.Fragment key={s.path}>
                <ChevronRight
                  className="size-3 text-muted-foreground"
                  aria-hidden
                />
                <BreadcrumbBtn
                  label={s.label}
                  onClick={() => void load(s.path)}
                />
              </React.Fragment>
            ))}
          </div>

          {/* 过滤 + 新建 + 隐藏 */}
          <div className="flex items-center gap-1.5">
            <Input
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder={t("remoteFs.filter.placeholder")}
              className="h-7 text-2xs"
              aria-label={t("remoteFs.filter.ariaLabel")}
            />
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-7 gap-1 text-2xs"
              onClick={() => setCreating(true)}
              disabled={creating}
            >
              <Plus className="size-3" /> {t("remoteFs.actions.newFolder")}
            </Button>
            <label className="flex items-center gap-1 text-2xs text-muted-foreground">
              <input
                type="checkbox"
                checked={showHidden}
                onChange={(e) => setShowHidden(e.target.checked)}
                aria-label={t("remoteFs.hidden.ariaLabel")}
              />
              {t("remoteFs.hidden.label")}
            </label>
          </div>

          {truncated ? (
            <div className="flex items-center gap-1 rounded-md border border-destructive bg-destructive-soft px-2 py-1 text-2xs text-destructive">
              <AlertTriangle className="size-3" /> {t("remoteFs.truncated")}
            </div>
          ) : null}

          {err ? (
            <div className="rounded-md border border-destructive bg-destructive-soft px-2 py-1 text-2xs text-destructive">
              {err}
            </div>
          ) : null}

          {/* 新建行 */}
          {creating ? (
            <div className="flex flex-col gap-1 rounded-md border border-primary bg-primary-soft p-1.5 text-2xs">
              <div className="flex items-center gap-1.5">
                <FolderIcon className="size-3 text-primary" />
                <Input
                  value={newName}
                  onChange={(e) => setNewName(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") void handleMkdir();
                    if (e.key === "Escape") {
                      setCreating(false);
                      setNewName("");
                      setNewErr(null);
                    }
                  }}
                  className="h-6 flex-1 px-1.5 py-0.5 font-mono text-2xs"
                  placeholder={t("remoteFs.create.placeholder")}
                  autoFocus
                />
                <button
                  type="button"
                  onClick={() => {
                    setCreating(false);
                    setNewName("");
                    setNewErr(null);
                  }}
                  aria-label={t("remoteFs.actions.cancel")}
                >
                  <X className="size-3" />
                </button>
              </div>
              {newErr ? <div className="text-destructive">{newErr}</div> : null}
            </div>
          ) : null}

          {/* 列表 */}
          <div className="min-h-[160px] overflow-auto rounded border border-border bg-card">
            {loading ? (
              <div className="flex items-center justify-center py-6 text-2xs text-muted-foreground">
                <Loader2 className="mr-1.5 size-3 animate-spin" />{" "}
                {t("remoteFs.loading")}
              </div>
            ) : filtered.length === 0 ? (
              <div className="py-6 text-center text-2xs text-muted-foreground">
                {t("remoteFs.empty")}
              </div>
            ) : (
              <ul>
                {filtered.map((e) => (
                  <EntryRow
                    key={e.name}
                    entry={e}
                    selected={selectedName === e.name}
                    onSelect={() => e.isDir && setSelectedName(e.name)}
                    onEnter={() =>
                      e.isDir && void load(joinPath(currentPath, e.name))
                    }
                  />
                ))}
              </ul>
            )}
          </div>
        </DialogBody>

        <DialogFooter className="flex items-center justify-between gap-2">
          <span
            className="min-w-0 flex-1 truncate font-mono text-2xs text-muted-foreground"
            title={finalPath}
          >
            {finalPath}
          </span>
          <Button
            type="button"
            variant="ghost"
            onClick={() => onOpenChange(false)}
          >
            {t("remoteFs.actions.cancel")}
          </Button>
          <Button
            type="button"
            onClick={() => {
              onPick(finalPath);
              onOpenChange(false);
            }}
          >
            {t("remoteFs.actions.selectCurrent")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function BreadcrumbBtn({
  label,
  onClick,
}: {
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="rounded px-1 py-0.5 font-mono text-foreground hover:bg-accent"
    >
      {label}
    </button>
  );
}

function EntryRow({
  entry,
  selected,
  onSelect,
  onEnter,
}: {
  entry: EntryView;
  selected: boolean;
  onSelect: () => void;
  onEnter: () => void;
}) {
  const { t } = useTranslation();
  const dim = !entry.isDir;
  return (
    <li
      className={cn(
        "flex items-center gap-2 px-2 py-1 text-2xs",
        dim && "cursor-default text-muted-foreground",
        !dim && "cursor-pointer hover:bg-accent",
        selected && "bg-primary-soft",
      )}
      onClick={() => !dim && onSelect()}
      onDoubleClick={() => !dim && onEnter()}
      data-testid={`entry-${entry.name}`}
    >
      {entry.symlink ? (
        <LinkIcon
          className="size-3 shrink-0 text-cyan-500"
          aria-label={t("remoteFs.entry.symlink")}
        />
      ) : entry.isDir ? (
        <FolderIcon className="size-3 shrink-0 text-amber-500" />
      ) : (
        <FileIcon className="size-3 shrink-0" />
      )}
      <span className="min-w-0 flex-1 truncate font-mono">{entry.name}</span>
      {entry.isDir ? (
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            onEnter();
          }}
          className="rounded px-1 py-0.5 text-2xs text-muted-foreground hover:bg-accent hover:text-foreground"
          aria-label={t("remoteFs.entry.enterNamed", { name: entry.name })}
        >
          {t("remoteFs.entry.enter")}
        </button>
      ) : null}
    </li>
  );
}

function joinPath(parent: string, name: string): string {
  if (parent.endsWith("/")) return parent + name;
  return parent + "/" + name;
}
