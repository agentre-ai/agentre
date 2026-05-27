import { useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Textarea } from "@/components/ui/textarea";

import { AgentreDialog } from "../app-dialog";

type Props = {
  open: boolean;
  initialMode: string;
  initialPEM: string;
  onClose: () => void;
  onApply: (mode: string, pem: string) => void | Promise<void>;
};

const PEM_HEADER = "-----BEGIN CERTIFICATE-----";
const PEM_FOOTER = "-----END CERTIFICATE-----";

function validatePEM(pem: string): string | null {
  const trimmed = pem.trim();
  if (!trimmed) return "请粘贴 PEM 内容";
  if (!trimmed.includes(PEM_HEADER) || !trimmed.includes(PEM_FOOTER)) {
    return "PEM 格式不正确：缺少 BEGIN/END CERTIFICATE 标记";
  }
  return null;
}

const MODES = [
  {
    value: "default",
    label: "默认",
    badge: "推荐",
    description: "走 OS 信任库；适用 mkcert -install / Let's Encrypt / 企业 CA",
  },
  {
    value: "pin-cert",
    label: "Pin 证书",
    description: "Pin agentred 的 leaf 证书；适合自签证书，不走 CA 框架",
  },
  {
    value: "ca-bundle",
    label: "CA 证书包",
    description: "信任指定 CA 签发的证书；适合企业 PKI 未装到 OS 信任库",
  },
  {
    value: "skip-verify",
    label: "跳过校验",
    badge: "不推荐",
    description:
      "禁用 TLS 校验；任何能访问到 agentred 的人都能冒充它，仅调试用",
    danger: true,
  },
];

export function TLSTrustDialog({
  open,
  initialMode,
  initialPEM,
  onClose,
  onApply,
}: Props) {
  const [mode, setMode] = useState(initialMode || "default");
  const [pem, setPem] = useState(initialPEM ?? "");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (open) {
      setMode(initialMode || "default");
      setPem(initialPEM ?? "");
      setError(null);
    }
  }, [open, initialMode, initialPEM]);

  const needsPEM = mode === "pin-cert" || mode === "ca-bundle";

  const apply = async () => {
    setError(null);
    if (needsPEM) {
      const e = validatePEM(pem);
      if (e) {
        setError(e);
        return;
      }
    }
    await onApply(mode, needsPEM ? pem.trim() : "");
  };

  return (
    <AgentreDialog
      open={open}
      onOpenChange={(o) => {
        if (!o) onClose();
      }}
      title="TLS 信任"
      description="如何验证 agentred 的 TLS 证书"
      contentClassName="sm:max-w-[540px]"
      bodyClassName="flex flex-col gap-3.5"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
          <Button onClick={apply}>应用</Button>
        </>
      }
    >
      <RadioGroup value={mode} onValueChange={setMode} className="gap-3">
        {MODES.map((m) => (
          <label
            key={m.value}
            className={`flex items-start gap-3 rounded-md border p-3 cursor-pointer ${
              mode === m.value
                ? m.danger
                  ? "border-destructive bg-destructive/5"
                  : "border-primary bg-primary/5"
                : "border-border"
            }`}
          >
            <RadioGroupItem value={m.value} className="mt-1" />
            <div className="flex flex-col gap-1">
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium">{m.label}</span>
                {m.badge ? (
                  <span
                    className={`text-[10px] uppercase tracking-wide px-1.5 py-0.5 rounded ${
                      m.danger
                        ? "bg-destructive/20 text-destructive"
                        : "bg-secondary text-secondary-foreground"
                    }`}
                  >
                    {m.badge}
                  </span>
                ) : null}
              </div>
              <p className="text-xs text-muted-foreground">{m.description}</p>
            </div>
          </label>
        ))}
      </RadioGroup>

      {needsPEM ? (
        <label className="flex flex-col gap-1.5">
          <span className="text-sm font-medium">
            {mode === "pin-cert" ? "证书（PEM）" : "CA 证书包（PEM）"}
          </span>
          <Textarea
            value={pem}
            onChange={(e) => setPem(e.target.value)}
            placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"
            rows={8}
            className="font-mono text-xs"
          />
        </label>
      ) : null}

      {error ? <div className="text-sm text-destructive">{error}</div> : null}
    </AgentreDialog>
  );
}
