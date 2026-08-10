import { useEffect, useState } from "react";
import { Plus, Server } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";

import { AddDeviceDialog } from "./add-device-dialog";
import { DeviceRow } from "./device-row";
import { hostOf } from "./format";
import { LoginDialog } from "./login-dialog";
import { TLSTrustDialog } from "./tls-trust-dialog";
import { useRemoteDevices, type DeviceRowModel } from "./use-remote-devices";
import { useServerLogin } from "./use-server-login";

// 规格「界面与交互 › 登录」: entry point for the standard device-authorization
// flow, and the signed-in identity + sign-out affordance once connected. The
// remote devices panel is the natural host — it already renders the unclaimed
// marker and claim guidance that this flow closes the loop on.
function AccountStatus({
  loading,
  loggedIn,
  serverURL,
  onSignIn,
  onSignOut,
}: {
  loading: boolean;
  loggedIn: boolean;
  serverURL: string;
  onSignIn: () => void;
  onSignOut: () => void;
}) {
  const { t } = useTranslation();

  if (loading) return null;

  if (!loggedIn) {
    return (
      <Button variant="outline" onClick={onSignIn}>
        {t("remoteDevices.login.signIn")}
      </Button>
    );
  }

  return (
    <div className="flex items-center gap-2 text-sm text-muted-foreground">
      <span>
        {t("remoteDevices.login.status.signedIn", {
          server: hostOf(serverURL),
        })}
      </span>
      <Button variant="ghost" size="sm" onClick={onSignOut}>
        {t("remoteDevices.login.actions.signOut")}
      </Button>
    </div>
  );
}

export function RemoteDevicesPanel() {
  const { t } = useTranslation();
  const { devices, loading, add, remove, updateTLS, rename, refresh, reload } =
    useRemoteDevices();
  const serverLogin = useServerLogin();
  const [now, setNow] = useState(() => Date.now());
  const [addOpen, setAddOpen] = useState(false);
  const [loginOpen, setLoginOpen] = useState(false);
  const [editTLSFor, setEditTLSFor] = useState<DeviceRowModel | null>(null);
  const [removeFor, setRemoveFor] = useState<DeviceRowModel | null>(null);
  const [renameFor, setRenameFor] = useState<{
    id: number;
    draft: string;
  } | null>(null);

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
        <div className="flex items-center gap-2">
          <AccountStatus
            loading={serverLogin.loading}
            loggedIn={serverLogin.loggedIn}
            serverURL={serverLogin.state?.ServerURL ?? ""}
            onSignIn={() => setLoginOpen(true)}
            onSignOut={() => {
              // logout() 如实抛出 ServerLogout 的失败,而它已经在自己的 finally
              // 里把登录状态重读过了 —— 失败时账号仍是登录态,头部照实继续显示
              // 「已登录」。这里只负责让设备列表也跟着重读,并且不留下一个
              // unhandled rejection(过去 .then 链上没有任何 catch)。
              void serverLogin
                .logout()
                .catch(() => {})
                .finally(() => reload());
            }}
          />
          <Button onClick={() => setAddOpen(true)}>
            <Plus className="mr-2 h-4 w-4" />{" "}
            {t("remoteDevices.actions.addAgentred")}
          </Button>
        </div>
      </header>

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
              onRename={() => setRenameFor({ id: d.id, draft: d.name })}
              onEditTLS={() => setEditTLSFor(d)}
              onRemove={() => setRemoveFor(d)}
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

      <Dialog
        open={removeFor !== null}
        onOpenChange={(open) => {
          if (!open) setRemoveFor(null);
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>
              {t("remoteDevices.actions.removePairing")}
            </DialogTitle>
          </DialogHeader>
          <DialogBody>
            <p className="text-sm text-muted-foreground">
              {t("remoteDevices.actions.removeConfirm", {
                name: removeFor?.name ?? "",
              })}
            </p>
          </DialogBody>
          <DialogFooter>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setRemoveFor(null)}
            >
              {t("common.cancel")}
            </Button>
            <Button
              size="sm"
              variant="destructive"
              onClick={() => {
                if (removeFor) void remove(removeFor.id);
                setRemoveFor(null);
              }}
            >
              {t("remoteDevices.actions.removePairing")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={renameFor !== null}
        onOpenChange={(open) => {
          if (!open) setRenameFor(null);
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("remoteDevices.actions.rename")}</DialogTitle>
          </DialogHeader>
          <DialogBody>
            <form
              id="rename-device-form"
              onSubmit={(e) => {
                e.preventDefault();
                if (renameFor && renameFor.draft.trim()) {
                  void rename(renameFor.id, renameFor.draft.trim());
                  setRenameFor(null);
                }
              }}
            >
              <Input
                autoFocus
                value={renameFor?.draft ?? ""}
                onChange={(e) =>
                  setRenameFor((prev) =>
                    prev ? { ...prev, draft: e.target.value } : prev,
                  )
                }
                placeholder={t("remoteDevices.actions.renamePrompt")}
                aria-label={t("remoteDevices.actions.renamePrompt")}
              />
            </form>
          </DialogBody>
          <DialogFooter>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setRenameFor(null)}
            >
              {t("common.cancel")}
            </Button>
            <Button
              type="submit"
              form="rename-device-form"
              size="sm"
              disabled={!renameFor || renameFor.draft.trim().length === 0}
            >
              {t("common.save")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

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

      <LoginDialog
        open={loginOpen}
        initialUrl={serverLogin.state?.ServerURL ?? ""}
        onClose={() => setLoginOpen(false)}
        onLoggedIn={() => {
          // Login completed inside the dialog — pull the fresh identity and
          // re-merge the device list so unclaimed markers / relay chips
          // reflect the newly-claimed account without waiting for a window
          // focus event.
          void serverLogin.refresh();
          void reload();
        }}
        checkURL={serverLogin.checkURL}
        startLogin={serverLogin.startLogin}
        pollLoginToken={serverLogin.pollLoginToken}
        cancelLogin={serverLogin.cancelLogin}
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
