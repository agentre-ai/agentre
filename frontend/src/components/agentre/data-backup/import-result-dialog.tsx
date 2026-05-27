import { CheckCircle2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

const LABEL: Record<string, string> = {
  created: "新建",
  overwrote: "覆盖",
  skipped: "跳过",
  duplicated: "复制",
};

export function ImportResultDialog({
  counts,
  onClose,
}: {
  counts: Record<string, number> | null;
  onClose: () => void;
}) {
  if (!counts) return null;
  const open = counts !== null;
  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <CheckCircle2 className="size-5 text-primary" />
            导入完成
          </DialogTitle>
        </DialogHeader>
        <ul className="space-y-1 text-sm">
          {Object.entries(counts).map(([k, v]) => (
            <li key={k}>
              <span className="text-muted-foreground">{LABEL[k] ?? k}:</span>{" "}
              {v}
            </li>
          ))}
        </ul>
        <DialogFooter>
          <Button onClick={onClose}>知道了</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
