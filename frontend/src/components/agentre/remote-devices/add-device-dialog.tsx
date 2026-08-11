import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";

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
      <div className="rounded-md bg-secondary/50 px-3 py-2 text-xs text-muted-foreground">
        {t("remoteDevices.add.instructions.prefix")}{" "}
        <code data-selectable-text="true" className="select-text">
          {t("remoteDevices.onboarding.commands.pair")}
        </code>
        {t("remoteDevices.add.instructions.suffix")}
      </div>

      <DevicePairingFields pairing={pairing} />
    </AgentreDialog>
  );
}
