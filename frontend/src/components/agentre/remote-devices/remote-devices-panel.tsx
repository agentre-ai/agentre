import { useEffect, useState } from "react";
import { Plus, Server } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";

import { AddDeviceDialog } from "./add-device-dialog";
import { DeviceRow } from "./device-row";
import { TLSTrustDialog } from "./tls-trust-dialog";
import { useRemoteDevices, type DeviceView } from "./use-remote-devices";

export function RemoteDevicesPanel() {
  const { t } = useTranslation();
  const { devices, loading, add, remove, updateTLS, rename, refresh } =
    useRemoteDevices();
  const [now, setNow] = useState(() => Date.now());
  const [addOpen, setAddOpen] = useState(false);
  const [editTLSFor, setEditTLSFor] = useState<DeviceView | null>(null);

  useEffect(() => {
    const t = window.setInterval(() => setNow(Date.now()), 60_000);
    return () => window.clearInterval(t);
  }, []);

  const onlineCount = devices.filter((d) => d.online).length;

  if (loading) return null;

  return (
    <div className="flex flex-col gap-4">
      <header className="flex items-start justify-between gap-4">
        <div className="flex flex-col gap-1">
          <h1 className="text-2xl font-semibold">
            {t("remoteDevices.panel.title")}
          </h1>
          <p className="text-sm text-muted-foreground">
            {t("remoteDevices.panel.description")}
          </p>
          {devices.length > 0 ? (
            <p className="text-xs text-muted-foreground">
              {t("remoteDevices.panel.stats", {
                paired: devices.length,
                online: onlineCount,
              })}
            </p>
          ) : null}
        </div>
        <Button onClick={() => setAddOpen(true)}>
          <Plus className="mr-2 h-4 w-4" />{" "}
          {t("remoteDevices.actions.addAgentred")}
        </Button>
      </header>

      <div className="flex items-center gap-2">
        <Badge variant="secondary">{t("remoteDevices.panel.lanAll")}</Badge>
      </div>

      {devices.length === 0 ? (
        <EmptyState onAdd={() => setAddOpen(true)} />
      ) : (
        <div className="flex flex-col gap-2">
          {devices.map((d) => (
            <DeviceRow
              key={d.id}
              device={d}
              now={now}
              onRefresh={() => void refresh(d.id)}
              onRename={() => {
                const next = window.prompt(
                  t("remoteDevices.actions.renamePrompt"),
                  d.name,
                );
                if (next && next.trim()) void rename(d.id, next.trim());
              }}
              onEditTLS={() => setEditTLSFor(d)}
              onRemove={() => {
                if (
                  window.confirm(
                    t("remoteDevices.actions.removeConfirm", {
                      name: d.name,
                    }),
                  )
                ) {
                  void remove(d.id);
                }
              }}
            />
          ))}
          <button
            type="button"
            onClick={() => setAddOpen(true)}
            className="text-sm text-muted-foreground hover:text-foreground self-start"
          >
            {t("remoteDevices.actions.continueAddLan")}
          </button>
        </div>
      )}

      <AddDeviceDialog
        open={addOpen}
        onClose={() => setAddOpen(false)}
        onSubmit={async (req) => {
          await add(req);
          setAddOpen(false);
        }}
      />

      <TLSTrustDialog
        open={editTLSFor !== null}
        initialMode={editTLSFor?.tlsMode ?? "default"}
        initialPEM={editTLSFor?.tlsCertPEM ?? ""}
        onClose={() => setEditTLSFor(null)}
        onApply={async (mode, pem) => {
          if (editTLSFor) {
            await updateTLS(editTLSFor.id, mode, pem);
          }
          setEditTLSFor(null);
        }}
      />
    </div>
  );
}

function EmptyState({ onAdd }: { onAdd: () => void }) {
  const { t } = useTranslation();

  return (
    <div className="flex flex-col items-center gap-3 rounded-lg border border-dashed py-12 px-6 text-center">
      <Server className="h-10 w-10 text-muted-foreground" />
      <div className="text-base font-medium">
        {t("remoteDevices.empty.title")}
      </div>
      <div className="text-sm text-muted-foreground max-w-md">
        {t("remoteDevices.empty.prefix")} <code>agentred run</code>
        {t("remoteDevices.empty.middle")} <code>agentred pair</code>{" "}
        {t("remoteDevices.empty.suffix")}
      </div>
      <Button onClick={onAdd}>
        <Plus className="mr-2 h-4 w-4" />{" "}
        {t("remoteDevices.actions.addAgentred")}
      </Button>
    </div>
  );
}
