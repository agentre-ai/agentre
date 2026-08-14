import { useEffect, useState } from "react";
import { ArrowRight, CircleCheck, Plus } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
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
import { Spinner } from "@/components/ui/spinner";

import { DesktopDeviceRow } from "./desktop-device-row";
import { AgentredOnboarding } from "./agentred-onboarding";
import { DeviceRow } from "./device-row";
import { hostOf } from "./format";
import { LoginDialog } from "./login-dialog";
import { TLSTrustDialog } from "./tls-trust-dialog";
import { useRemoteDevices, type DeviceView } from "./use-remote-devices";
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

export type RemoteDevicesPanelProps = {
  onOpenAgentBackends?: () => void;
};

export function RemoteDevicesPanel({
  onOpenAgentBackends = () => {},
}: RemoteDevicesPanelProps) {
  const { t } = useTranslation();
  const {
    devices,
    accountDevices,
    loadState,
    add,
    remove,
    updateTLS,
    rename,
    refresh,
    reload,
  } = useRemoteDevices();
  const serverLogin = useServerLogin();
  const [now, setNow] = useState(() => Date.now());
  const [guideRequested, setGuideRequested] = useState(false);
  const [showBackendGuide, setShowBackendGuide] = useState(false);
  const [loginOpen, setLoginOpen] = useState(false);
  // 改 TLS / 删配对都作用在 LAN 配对行上,这两个对话框拿的就是那一行。
  const [editTLSFor, setEditTLSFor] = useState<DeviceView | null>(null);
  const [removeFor, setRemoveFor] = useState<DeviceView | null>(null);
  const [renameFor, setRenameFor] = useState<{
    id: number;
    draft: string;
  } | null>(null);

  useEffect(() => {
    const t = window.setInterval(() => setNow(Date.now()), 60_000);
    return () => window.clearInterval(t);
  }, []);

  const onlineCount = devices.filter((d) => d.online).length;
  // 决策 1:零设备时引导就是这一页(且无处可退,不给收起);已有设备时它默认收起,
  // 由页头那唯一一个入口召唤。加载中/加载失败在下面提前返回,两种情况都到不了这里。
  const guideDismissible = devices.length > 0;
  const guideOpen = !guideDismissible || guideRequested;
  // 账号设备清单里的桌面端（R19）：不参与 LAN 配对行合并，单独作为可展开行。
  const desktopDevices = (accountDevices ?? []).filter(
    (d) => d.Kind === "desktop",
  );
  const reloadInBackground = () => {
    void reload().catch(() => {});
  };

  if (loadState === "loading") {
    return (
      <div className="flex min-h-48 items-center justify-center">
        <Spinner
          className="size-5"
          aria-label={t("remoteDevices.load.loading")}
        />
      </div>
    );
  }

  if (loadState === "error") {
    return (
      <div
        role="alert"
        className="flex flex-col items-start gap-3 rounded-lg border border-status-error/40 bg-destructive-soft p-4"
      >
        <div className="flex flex-col gap-1">
          <p className="font-medium text-status-error">
            {t("remoteDevices.load.errorTitle")}
          </p>
          <p className="text-sm text-muted-foreground">
            {t("remoteDevices.load.errorDescription")}
          </p>
        </div>
        <Button variant="outline" onClick={reloadInBackground}>
          {t("common.retry")}
        </Button>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <header className="flex flex-col items-start justify-between gap-4 sm:flex-row">
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
        <div className="flex flex-wrap items-center gap-2">
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
                .finally(reloadInBackground);
            }}
          />
          {/* 引导开着时不渲染:这个按钮的唯一职责就是打开引导。 */}
          {guideOpen ? null : (
            <Button onClick={() => setGuideRequested(true)}>
              <Plus data-icon="inline-start" aria-hidden="true" />
              {t("remoteDevices.actions.addAgentred")}
            </Button>
          )}
        </div>
      </header>

      {guideOpen ? (
        <AgentredOnboarding
          onDismiss={
            guideDismissible ? () => setGuideRequested(false) : undefined
          }
          onSubmit={async (request) => {
            await add(request);
            setGuideRequested(false);
            // 决策 7:每一台配对成功都衔接到 Agent 后端,不只有第一台。
            setShowBackendGuide(true);
          }}
        />
      ) : null}

      {showBackendGuide ? (
        <Alert className="border-primary/30 bg-primary-soft">
          <CircleCheck aria-hidden="true" />
          <AlertTitle>
            {t("remoteDevices.onboarding.complete.title")}
          </AlertTitle>
          <AlertDescription className="flex flex-col items-start justify-between gap-3 sm:flex-row sm:items-center">
            <span>{t("remoteDevices.onboarding.complete.description")}</span>
            <Button type="button" size="sm" onClick={onOpenAgentBackends}>
              {t("remoteDevices.onboarding.complete.action")}
              <ArrowRight data-icon="inline-end" aria-hidden="true" />
            </Button>
          </AlertDescription>
        </Alert>
      ) : null}

      {devices.length > 0 ? (
        <div className="flex flex-col gap-2">
          {devices.map((d) => {
            // 用 const 接住,窄化才进得了下面那几个闭包。
            const lan = d.lan;
            return (
              <DeviceRow
                key={d.key}
                device={d}
                now={now}
                actions={
                  lan
                    ? {
                        onRefresh: () => void refresh(lan.id),
                        onRename: () =>
                          setRenameFor({ id: lan.id, draft: lan.name }),
                        onEditTLS: () => setEditTLSFor(lan),
                        onRemove: () => setRemoveFor(lan),
                      }
                    : undefined
                }
              />
            );
          })}
        </div>
      ) : null}

      {desktopDevices.length > 0 ? (
        <div className="flex flex-col gap-2">
          {/* 账号设备清单里 kind=desktop 的行（R19）：正在运行的桌面端可展开出会话
              列表，未运行时按 R2 说明「Agentre 未运行」而不是「离线」。 */}
          {desktopDevices.map((d) => (
            <DesktopDeviceRow key={d.ID} device={d} now={now} />
          ))}
        </div>
      ) : null}

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
          reloadInBackground();
        }}
        checkURL={serverLogin.checkURL}
        startLogin={serverLogin.startLogin}
        pollLoginToken={serverLogin.pollLoginToken}
        cancelLogin={serverLogin.cancelLogin}
      />
    </div>
  );
}
