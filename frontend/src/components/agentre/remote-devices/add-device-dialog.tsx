import { useState } from "react";
import { ChevronRight } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

import { AgentreDialog } from "../app-dialog";
import { TLSTrustDialog } from "./tls-trust-dialog";
import { deriveDeviceName } from "./format";

// Local type — mirrors remote_device_svc.AddRequest; avoids transitive wailsjs import.
type AddRequest = {
  url: string;
  pairingCode: string;
  displayName?: string;
  tlsMode: string;
  tlsCertPEM?: string;
};

const URL_RE = /^wss?:\/\/[^/]+\/rpc$/;

function limitCode(raw: string): string {
  return raw.toUpperCase().slice(0, 6);
}

type Props = {
  open: boolean;
  onClose: () => void;
  onSubmit: (req: AddRequest) => Promise<void>;
};

export function AddDeviceDialog({ open, onClose, onSubmit }: Props) {
  const [url, setUrl] = useState("");
  const [code, setCode] = useState("");
  const [name, setName] = useState("");
  const [tlsMode, setTlsMode] = useState("default");
  const [tlsCertPEM, setTlsCertPEM] = useState("");
  const [tlsOpen, setTlsOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const urlValid = URL_RE.test(url);
  const codeValid = code.trim().length === 6;
  const canSubmit = urlValid && codeValid && !submitting;

  const reset = () => {
    setUrl("");
    setCode("");
    setName("");
    setTlsMode("default");
    setTlsCertPEM("");
    setError(null);
    setSubmitting(false);
  };
  const handleClose = () => {
    if (submitting) return;
    reset();
    onClose();
  };

  const submit = async () => {
    setError(null);
    setSubmitting(true);
    try {
      const effectiveName = name.trim() || deriveDeviceName(url, []);
      await onSubmit({
        url: url,
        pairingCode: code.trim(),
        displayName: effectiveName,
        tlsMode: tlsMode,
        tlsCertPEM: tlsCertPEM,
      });
      reset();
    } catch (e: unknown) {
      const msg =
        typeof e === "string" ? e : e instanceof Error ? e.message : "配对失败";
      setError(msg);
      setSubmitting(false);
    }
  };

  return (
    <>
      <AgentreDialog
        open={open}
        onOpenChange={(o) => {
          if (!o) handleClose();
        }}
        title="添加 agentred 设备"
        description="LAN 直连"
        contentClassName="sm:max-w-[540px]"
        bodyClassName="flex flex-col gap-3.5"
        footer={
          <>
            <Button variant="ghost" onClick={handleClose} disabled={submitting}>
              取消
            </Button>
            <Button onClick={submit} disabled={!canSubmit}>
              {submitting ? "配对中…" : "配对"}
            </Button>
          </>
        }
      >
        <div className="rounded-md bg-secondary/50 px-3 py-2 text-xs text-muted-foreground">
          在远端执行 <code>agentred pair</code>，填入打印的 URL 和 6 位配对码。
        </div>

        <label className="flex flex-col gap-1.5">
          <span className="text-sm font-medium">地址</span>
          <Input
            value={url}
            onChange={(e) => setUrl(e.target.value.trim())}
            placeholder="ws://192.168.1.100:7456/rpc"
            disabled={submitting}
            aria-invalid={url.length > 0 && !urlValid}
          />
        </label>

        <label className="flex flex-col gap-1.5">
          <span className="text-sm font-medium">配对码</span>
          <Input
            value={code}
            onChange={(e) => setCode(limitCode(e.target.value))}
            placeholder="ABC2DE"
            maxLength={6}
            disabled={submitting}
            className="font-mono tracking-widest text-center text-lg"
          />
          <span className="text-xs text-muted-foreground">6 字符配对码</span>
        </label>

        <label className="flex flex-col gap-1.5">
          <span className="text-sm font-medium">显示名称（可选）</span>
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="留空则自动从地址派生"
            disabled={submitting}
          />
        </label>

        <button
          type="button"
          className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground self-start"
          onClick={() => setTlsOpen(true)}
          disabled={submitting}
        >
          <ChevronRight className="h-4 w-4" />
          高级 — TLS 信任（{tlsMode === "default" ? "OS 默认" : tlsMode}）
        </button>

        {error ? <div className="text-sm text-destructive">{error}</div> : null}
      </AgentreDialog>

      <TLSTrustDialog
        open={tlsOpen}
        initialMode={tlsMode}
        initialPEM={tlsCertPEM}
        onClose={() => setTlsOpen(false)}
        onApply={(mode, pem) => {
          setTlsMode(mode);
          setTlsCertPEM(pem);
          setTlsOpen(false);
        }}
      />
    </>
  );
}
