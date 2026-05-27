import * as React from "react";

import { ExportSection } from "./export-section";
import { ImportResultDialog } from "./import-result-dialog";
import { ImportSection } from "./import-section";

export function DataBackupPanel() {
  const [result, setResult] = React.useState<Record<string, number> | null>(
    null,
  );
  return (
    <>
      <div className="flex max-w-3xl flex-col gap-1.5">
        <h1 className="text-2xl font-semibold tracking-normal">数据 & 备份</h1>
        <p className="text-sm leading-relaxed text-muted-foreground">
          在多台设备之间导入导出 Agentre 的配置数据。
        </p>
      </div>
      <ExportSection />
      <ImportSection onResult={setResult} />
      <ImportResultDialog counts={result} onClose={() => setResult(null)} />
    </>
  );
}
