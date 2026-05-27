import * as React from "react";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { ShieldAlert } from "lucide-react";

import type { ItemAction, Scope } from "./types";
import { SCOPE_LABELS } from "./types";

export type PreviewItem = {
  scope: Scope;
  sourceKey: string;
  name: string;
  conflict?: boolean;
  localID?: number;
  localName?: string;
  dangling?: boolean;
  danglingHint?: string;
  defaultAction: ItemAction;
};

export type ImportPreviewDialogProps = {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  preview: { secretsIncluded: boolean; items: PreviewItem[] } | null;
  onApply: (
    actions: Record<string, ItemAction>,
    fallback: ItemAction,
  ) => Promise<void>;
};

const ACTION_LABEL: Record<ItemAction, string> = {
  create: "新建",
  overwrite: "覆盖",
  skip: "跳过",
  duplicate: "复制为新",
};

export function ImportPreviewDialog({
  open,
  onOpenChange,
  preview,
  onApply,
}: ImportPreviewDialogProps) {
  const [global, setGlobal] = React.useState<ItemAction>("skip");
  const [actions, setActions] = React.useState<Record<string, ItemAction>>({});

  React.useEffect(() => {
    if (!preview) return;
    const next: Record<string, ItemAction> = {};
    for (const it of preview.items) {
      next[`${it.scope}:${it.sourceKey}`] = it.defaultAction;
    }
    // eslint-disable-next-line react-hooks/set-state-in-effect -- derive row actions from new preview prop
    setActions(next);
  }, [preview]);

  if (!preview) return null;

  const setOne = (key: string, v: ItemAction) =>
    setActions((p) => ({ ...p, [key]: v }));

  const setAllConflicts = (v: ItemAction) => {
    setGlobal(v);
    setActions((prev) => {
      const next = { ...prev };
      for (const it of preview.items) {
        if (it.conflict && !it.dangling) {
          next[`${it.scope}:${it.sourceKey}`] = v;
        }
      }
      return next;
    });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl">
        <DialogHeader>
          <DialogTitle>导入预览</DialogTitle>
          <DialogDescription>
            选好策略后点「应用」执行，失败会整批回滚。
          </DialogDescription>
        </DialogHeader>
        <DialogBody className="flex max-h-[70vh] flex-col gap-3 overflow-hidden">
          {!preview.secretsIncluded && (
            <Alert>
              <ShieldAlert className="size-4" />
              <AlertDescription>
                此文件不含凭证，导入后需要手动重新填写 API Key 等敏感字段。
              </AlertDescription>
            </Alert>
          )}
          <div className="flex items-center gap-2">
            <span className="text-sm text-muted-foreground">
              冲突项默认策略
            </span>
            <Select
              value={global}
              onValueChange={(v) => setAllConflicts(v as ItemAction)}
            >
              <SelectTrigger className="w-[160px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="skip">{ACTION_LABEL.skip}</SelectItem>
                <SelectItem value="overwrite">
                  {ACTION_LABEL.overwrite}
                </SelectItem>
                <SelectItem value="duplicate">
                  {ACTION_LABEL.duplicate}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>范围</TableHead>
                  <TableHead>名称</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>动作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {preview.items.map((it) => {
                  const key = `${it.scope}:${it.sourceKey}`;
                  const cur = actions[key] ?? it.defaultAction;
                  return (
                    <TableRow key={key}>
                      <TableCell className="text-xs">
                        {SCOPE_LABELS[it.scope]}
                      </TableCell>
                      <TableCell>{it.name}</TableCell>
                      <TableCell>
                        {it.dangling ? (
                          <Badge variant="destructive">引用缺失</Badge>
                        ) : it.conflict ? (
                          <Badge variant="secondary">冲突</Badge>
                        ) : (
                          <Badge>新增</Badge>
                        )}
                        {it.danglingHint && (
                          <div className="text-xs text-destructive mt-1">
                            {it.danglingHint}
                          </div>
                        )}
                      </TableCell>
                      <TableCell>
                        <Select
                          value={cur}
                          disabled={it.dangling}
                          onValueChange={(v) => setOne(key, v as ItemAction)}
                        >
                          <SelectTrigger className="w-[120px]">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            {!it.conflict && (
                              <SelectItem value="create">
                                {ACTION_LABEL.create}
                              </SelectItem>
                            )}
                            {it.conflict && (
                              <SelectItem value="overwrite">
                                {ACTION_LABEL.overwrite}
                              </SelectItem>
                            )}
                            <SelectItem value="skip">
                              {ACTION_LABEL.skip}
                            </SelectItem>
                            {!it.dangling && (
                              <SelectItem value="duplicate">
                                {ACTION_LABEL.duplicate}
                              </SelectItem>
                            )}
                          </SelectContent>
                        </Select>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </div>
        </DialogBody>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button onClick={() => onApply(actions, global)}>应用</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
