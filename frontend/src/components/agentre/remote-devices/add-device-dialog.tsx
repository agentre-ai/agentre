import { useState } from "react";
import { ChevronRight } from "lucide-react";
import { useTranslation } from "react-i18next";

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
  const { t } = useTranslation();
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
        typeof e === "string"
          ? e
          : e instanceof Error
            ? e.message
            : t("remoteDevices.add.errors.pairFailed");
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
        title={t("remoteDevices.add.title")}
        description={t("remoteDevices.add.description")}
        contentClassName="sm:max-w-[540px]"
        bodyClassName="flex flex-col gap-3.5"
        footer={
          <>
            <Button variant="ghost" onClick={handleClose} disabled={submitting}>
              {t("common.cancel")}
            </Button>
            <Button onClick={submit} disabled={!canSubmit}>
              {submitting
                ? t("remoteDevices.add.pairing")
                : t("remoteDevices.add.pair")}
            </Button>
          </>
        }
      >
        <div className="rounded-md bg-secondary/50 px-3 py-2 text-xs text-muted-foreground">
          {t("remoteDevices.add.instructions.prefix")}{" "}
          <code>agentred pair</code>
          {t("remoteDevices.add.instructions.suffix")}
        </div>

        <label className="flex flex-col gap-1.5">
          <span className="text-sm font-medium">
            {t("remoteDevices.add.fields.url")}
          </span>
          <Input
            value={url}
            onChange={(e) => setUrl(e.target.value.trim())}
            placeholder="ws://192.168.1.100:7456/rpc"
            disabled={submitting}
            aria-invalid={url.length > 0 && !urlValid}
          />
        </label>

        <label className="flex flex-col gap-1.5">
          <span className="text-sm font-medium">
            {t("remoteDevices.add.fields.pairingCode")}
          </span>
          <Input
            value={code}
            onChange={(e) => setCode(limitCode(e.target.value))}
            placeholder="ABC2DE"
            maxLength={6}
            disabled={submitting}
            className="font-mono tracking-widest text-center text-lg"
          />
          <span className="text-xs text-muted-foreground">
            {t("remoteDevices.add.fields.pairingCodeHint")}
          </span>
        </label>

        <label className="flex flex-col gap-1.5">
          <span className="text-sm font-medium">
            {t("remoteDevices.add.fields.displayName")}
          </span>
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={t("remoteDevices.add.fields.displayNamePlaceholder")}
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
          {t("remoteDevices.add.advancedTls", {
            mode:
              tlsMode === "default"
                ? t("remoteDevices.tls.modes.default.label")
                : tlsMode,
          })}
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
