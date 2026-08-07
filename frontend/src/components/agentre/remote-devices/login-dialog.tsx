import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

import { AgentreDialog } from "../app-dialog";
import { BrowserOpenURL } from "../../../../wailsjs/runtime/runtime";
import { friendlyLoginError } from "./format";

// Local type — mirrors server_svc.StartLoginResult; avoids transitive
// wailsjs import (same precedent as AddRequest in add-device-dialog.tsx).
export type StartLoginResult = {
  DeviceCode: string;
  UserCode: string;
  VerificationURI: string;
  VerificationURIComplete: string;
  Interval: number;
  ExpiresIn: number;
};

type Phase = "form" | "starting" | "waiting";

// RFC 8628 device flow: Interval is the server-mandated poll cadence in
// seconds; a floor guards against a degenerate 0/negative value hammering
// the server.
const MIN_INTERVAL_MS = 1000;
const FALLBACK_INTERVAL_S = 5;

type Props = {
  open: boolean;
  /** Prefills the URL field from the last-used server_state row, if any. */
  initialUrl: string;
  onClose: () => void;
  /** Called once PollLoginToken reports completion, before onClose. */
  onLoggedIn: () => void;
  checkURL: (url: string) => Promise<string>;
  startLogin: (url: string) => Promise<StartLoginResult>;
  pollLoginToken: (deviceCode: string) => Promise<boolean>;
  cancelLogin: () => Promise<void>;
};

export function LoginDialog({
  open,
  initialUrl,
  onClose,
  onLoggedIn,
  checkURL,
  startLogin,
  pollLoginToken,
  cancelLogin,
}: Props) {
  const { t } = useTranslation();
  const [url, setUrl] = useState(initialUrl);
  const [phase, setPhase] = useState<Phase>("form");
  const [login, setLogin] = useState<StartLoginResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const timerRef = useRef<number | null>(null);
  const deadlineRef = useRef<number | null>(null);
  // attemptRef 标识「当前这一次登录尝试」。清 interval 只挡得住**还没发出**的轮询;
  // 已经在飞的那一次 pollLoginToken 照样会 resolve/reject,它落定时必须先确认自己
  // 还属于当前这次尝试 —— 否则一次已经取消(且已发过 cancelLogin)的登录会凭一个
  // 迟到的 true 跑完 onLoggedIn(),或者把错误写进一个已经关掉的对话框。
  const attemptRef = useRef(0);

  const stopPolling = () => {
    attemptRef.current += 1;
    if (timerRef.current !== null) {
      window.clearInterval(timerRef.current);
      timerRef.current = null;
    }
    deadlineRef.current = null;
  };

  // Unmount safety net — every real close path already routes through
  // handleClose(), but guard against a leaked timer if the panel ever
  // unmounts the dialog outright instead of flipping `open`.
  useEffect(() => stopPolling, []);

  useEffect(() => {
    if (!open) {
      stopPolling();
      setPhase("form");
      setLogin(null);
      setError(null);
      setUrl(initialUrl);
    }
    // Re-sync only on the open/closed transition; initialUrl is read at that
    // moment rather than tracked continuously (same as TLSTrustDialog).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const poll = async (deviceCode: string, attempt: number) => {
    try {
      const done = await pollLoginToken(deviceCode);
      if (attempt !== attemptRef.current) return; // 取消 / 重开已经作废了这一次
      if (done) {
        stopPolling();
        onLoggedIn();
        onClose();
        return;
      }
      if (deadlineRef.current !== null && Date.now() >= deadlineRef.current) {
        stopPolling();
        setPhase("form");
        setError(t("remoteDevices.login.errors.expired"));
      }
    } catch (e: unknown) {
      if (attempt !== attemptRef.current) return;
      stopPolling();
      setPhase("form");
      setError(friendlyLoginError(e, t));
    }
  };

  const start = async () => {
    stopPolling(); // 作废上一次尝试还在飞的轮询,再开新的一次
    // 这一次尝试的身份**在起点就定下**,不能等两次 await 回来再读:stopPolling
    // (卸载清理 / 关闭 / 再点一次)只清得掉**已经存在**的 timer,清不掉一次还在
    // 飞的 start。若等 await 之后才读 attemptRef,读到的正是那次清理刚 +1 出来的
    // 新值,身份校验于是放行,而这个 interval 已经没有任何人能清 —— 轮询会一直
    // 跑到进程结束,一个迟到的 true 还会把 onLoggedIn/onClose 打进已经卸载的父组件。
    const attempt = attemptRef.current;
    const stale = () => attempt !== attemptRef.current;
    setError(null);
    setPhase("starting");
    try {
      await checkURL(url);
    } catch (e: unknown) {
      if (stale()) return;
      setPhase("form");
      setError(friendlyLoginError(e, t));
      return;
    }
    if (stale()) return;
    try {
      const res = await startLogin(url);
      if (stale()) return;
      setLogin(res);
      setPhase("waiting");
      deadlineRef.current = Date.now() + Math.max(0, res.ExpiresIn || 0) * 1000;
      const intervalMs =
        Math.max(1, res.Interval || FALLBACK_INTERVAL_S) * 1000;
      timerRef.current = window.setInterval(
        () => void poll(res.DeviceCode, attempt),
        Math.max(MIN_INTERVAL_MS, intervalMs),
      );
    } catch (e: unknown) {
      if (stale()) return;
      setPhase("form");
      setError(friendlyLoginError(e, t));
    }
  };

  const handleClose = () => {
    if (phase === "starting") return; // mid-flight — same guard as AddDeviceDialog's submitting.
    const wasWaiting = phase === "waiting";
    stopPolling();
    setPhase("form");
    setLogin(null);
    setError(null);
    if (wasWaiting) {
      void cancelLogin().catch(() => {});
    }
    onClose();
  };

  const canSubmit = url.trim().length > 0 && phase !== "starting";

  return (
    <AgentreDialog
      open={open}
      onOpenChange={(o) => {
        if (!o) handleClose();
      }}
      title={t("remoteDevices.login.title")}
      description={t("remoteDevices.login.description")}
      contentClassName="sm:max-w-[480px]"
      bodyClassName="flex flex-col gap-3.5"
      footer={
        <>
          <Button
            variant="ghost"
            onClick={handleClose}
            disabled={phase === "starting"}
          >
            {t("common.cancel")}
          </Button>
          {phase !== "waiting" ? (
            <Button onClick={start} disabled={!canSubmit}>
              {phase === "starting"
                ? t("remoteDevices.login.actions.connecting")
                : t("remoteDevices.login.actions.continue")}
            </Button>
          ) : null}
        </>
      }
    >
      {phase === "waiting" && login ? (
        <div className="flex flex-col gap-3">
          <div className="flex flex-col items-center gap-1 rounded-md bg-secondary/50 py-4">
            <span className="text-xs text-muted-foreground">
              {t("remoteDevices.login.waiting.userCode")}
            </span>
            <span className="font-mono text-2xl font-semibold tracking-widest">
              {login.UserCode}
            </span>
          </div>
          <div className="flex flex-col gap-1.5">
            <span className="text-xs text-muted-foreground">
              {t("remoteDevices.login.waiting.verificationUrl")}
            </span>
            <code className="break-all text-xs">{login.VerificationURI}</code>
          </div>
          <Button
            type="button"
            variant="secondary"
            onClick={() =>
              BrowserOpenURL(
                login.VerificationURIComplete || login.VerificationURI,
              )
            }
          >
            {t("remoteDevices.login.actions.openBrowser")}
          </Button>
        </div>
      ) : (
        <label className="flex flex-col gap-1.5">
          <span className="text-sm font-medium">
            {t("remoteDevices.login.fields.url")}
          </span>
          <Input
            value={url}
            onChange={(e) => setUrl(e.target.value.trim())}
            placeholder="https://hub.example.com"
            disabled={phase === "starting"}
          />
        </label>
      )}

      {error ? <div className="text-sm text-destructive">{error}</div> : null}
    </AgentreDialog>
  );
}
