import * as React from "react";
import { Download, ShieldAlert } from "lucide-react";
import { toast } from "sonner";

import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Switch } from "@/components/ui/switch";

import { ExportData } from "../../../../wailsjs/go/app/App";
import { Scope, SCOPE_LABELS } from "./types";

const ALL_SCOPES: Scope[] = [
  "llm-providers",
  "agent-backends",
  "organization",
  "remote-devices",
];

export function ExportSection() {
  const [selected, setSelected] = React.useState<Set<Scope>>(
    new Set(ALL_SCOPES),
  );
  const [includeSecrets, setIncludeSecrets] = React.useState(false);
  const [running, setRunning] = React.useState(false);

  const toggle = (s: Scope) => {
    const next = new Set(selected);
    if (next.has(s)) {
      next.delete(s);
    } else {
      next.add(s);
    }
    setSelected(next);
  };

  const handleExport = async () => {
    if (selected.size === 0) {
      toast.error("请至少选择一个范围");
      return;
    }
    setRunning(true);
    try {
      const res = await ExportData({
        scopes: Array.from(selected),
        includeSecrets,
      });
      if (res.canceled) return;
      const total = Object.values(res.summary ?? {}).reduce((a, b) => a + b, 0);
      toast.success(`已导出 ${total} 条数据`, { description: res.path });
    } catch (e) {
      toast.error("导出失败", { description: String(e) });
    } finally {
      setRunning(false);
    }
  };

  return (
    <section className="rounded-lg border border-border bg-card p-4 space-y-4">
      <header>
        <h2 className="text-sm font-semibold">导出</h2>
        <p className="text-xs text-muted-foreground">
          选择要导出的范围，生成 JSON 文件。
        </p>
      </header>
      <div className="grid grid-cols-2 gap-2">
        {ALL_SCOPES.map((s) => (
          <label key={s} className="flex items-center gap-2 text-sm">
            <Checkbox
              checked={selected.has(s)}
              onCheckedChange={() => toggle(s)}
              aria-label={SCOPE_LABELS[s]}
            />
            {SCOPE_LABELS[s]}
          </label>
        ))}
      </div>
      <div className="flex items-center justify-between rounded-md border border-border p-3">
        <div>
          <div className="text-sm font-medium">包含凭证</div>
          <div className="text-xs text-muted-foreground">
            勾选后导出 API Key / TLS 证书等明文。请妥善保管文件。
          </div>
        </div>
        <Switch checked={includeSecrets} onCheckedChange={setIncludeSecrets} />
      </div>
      {includeSecrets && (
        <Alert>
          <ShieldAlert className="size-4" />
          <AlertDescription>
            导出文件含明文凭证，任何拿到该文件的人都可使用你的账号。
          </AlertDescription>
        </Alert>
      )}
      <Button onClick={handleExport} disabled={running}>
        <Download className="size-4 mr-2" />
        导出...
      </Button>
    </section>
  );
}
