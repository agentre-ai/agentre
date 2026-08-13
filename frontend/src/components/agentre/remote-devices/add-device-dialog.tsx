import { Copy } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { copyTextWithToast } from "@/lib/clipboard-toast";

import { AgentreDialog } from "../app-dialog";
import {
  DevicePairingFields,
  PairingSubmitButton,
  type AddRequest,
  useDevicePairingForm,
} from "./device-pairing-form";

type Props = {
  open: boolean;
  onClose: () => void;
  onSubmit: (req: AddRequest) => Promise<void>;
};

export function AddDeviceDialog({ open, onClose, onSubmit }: Props) {
  const { t } = useTranslation();
  const pairing = useDevicePairingForm({ onSubmit });
  const pairCommand = t("remoteDevices.onboarding.commands.pair");

  const handleClose = () => {
    if (pairing.submitting) return;
    pairing.reset();
    onClose();
  };

  return (
    <AgentreDialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) handleClose();
      }}
      onSubmit={(event) => {
        event.preventDefault();
        void pairing.submit();
      }}
      title={t("remoteDevices.add.title")}
      description={t("remoteDevices.add.description")}
      contentClassName="sm:max-w-[540px]"
      bodyClassName="flex flex-col gap-3.5"
      footer={
        <>
          <Button
            type="button"
            variant="ghost"
            onClick={handleClose}
            disabled={pairing.submitting}
          >
            {t("common.cancel")}
          </Button>
          <PairingSubmitButton
            canSubmit={pairing.canSubmit}
            submitting={pairing.submitting}
          />
        </>
      }
    >
      <div className="flex items-center justify-between gap-3 rounded-md bg-secondary/50 px-3 py-2 text-xs text-muted-foreground">
        <span>
          {t("remoteDevices.add.instructions.prefix")}{" "}
          <code data-selectable-text="true" className="select-text">
            {pairCommand}
          </code>
          {t("remoteDevices.add.instructions.suffix")}
        </span>
        <Button
          type="button"
          variant="ghost"
          size="xs"
          className="shrink-0"
          aria-label={t("remoteDevices.onboarding.copyLabel", {
            label: pairCommand,
          })}
          onClick={() => {
            void copyTextWithToast(pairCommand, {
              successTitle: t("remoteDevices.onboarding.copySuccess"),
            });
          }}
        >
          <Copy data-icon="inline-start" aria-hidden="true" />
          {t("common.copy")}
        </Button>
      </div>

      <DevicePairingFields pairing={pairing} />
    </AgentreDialog>
  );
}
