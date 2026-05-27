import { useEffect, useState } from "react";
import { Plus, Server } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";

import { AddDeviceDialog } from "./add-device-dialog";
import { DeviceRow } from "./device-row";
import { TLSTrustDialog } from "./tls-trust-dialog";
import { useRemoteDevices, type DeviceView } from "./use-remote-devices";

export function RemoteDevicesPanel() {
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
          <h1 className="text-2xl font-semibold">远端</h1>
          <p className="text-sm text-muted-foreground">
            管理远程 agentred 计算端 — desktop 远程驱动；LAN 同网段直连，Cloud
            中转跨网段（v0.3+）。
          </p>
          {devices.length > 0 ? (
            <p className="text-xs text-muted-foreground">
              {devices.length} 已配对 · {onlineCount} 在线
            </p>
          ) : null}
        </div>
        <Button onClick={() => setAddOpen(true)}>
          <Plus className="mr-2 h-4 w-4" /> 添加 agentred
        </Button>
      </header>

      <div className="flex items-center gap-2">
        <Badge variant="secondary">LAN 直连 · 全部</Badge>
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
                const next = window.prompt("重命名为", d.name);
                if (next && next.trim()) void rename(d.id, next.trim());
              }}
              onEditTLS={() => setEditTLSFor(d)}
              onRemove={() => {
                if (
                  window.confirm(
                    `解除配对 "${d.name}"？本桌面将清空对该 agentred 的 token 与 fingerprint pin；远端 agentred 的 state.json.pairedPeers 不会自动清理。`,
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
            + 继续添加 agentred（LAN）
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
  return (
    <div className="flex flex-col items-center gap-3 rounded-lg border border-dashed py-12 px-6 text-center">
      <Server className="h-10 w-10 text-muted-foreground" />
      <div className="text-base font-medium">尚未配对任何 agentred</div>
      <div className="text-sm text-muted-foreground max-w-md">
        在远程机器执行 <code>agentred run</code>，再执行{" "}
        <code>agentred pair</code> 拿 6 位配对码，回到这里点「添加
        agentred」输入。
      </div>
      <Button onClick={onAdd}>
        <Plus className="mr-2 h-4 w-4" /> 添加 agentred
      </Button>
    </div>
  );
}
