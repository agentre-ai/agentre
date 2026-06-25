import { useEffect, useState, useCallback } from "react";
import * as App from "@/../wailsjs/go/app/App";
import { EventsOn, EventsOff } from "@/../wailsjs/runtime/runtime";
import { base64ToBytes } from "./base64";

type Reason =
  | "natural"
  | "killed"
  | "connection_lost"
  | "daemon_shutdown"
  | "error";
export type TerminalState = "opening" | "open" | "closing" | "idle";

export interface UseTerminalArgs {
  terminalID: string;
  projectId: number;
  deviceId: string;
  cols: number;
  rows: number;
  // enabled=false 时本 hook 完全 inert:不 EventsOn / TerminalOpen,卸载也不 TerminalClose。
  // attach 模式(接管别处已起的 PTY)用它把 live 路径关掉。默认启用,既有调用方不受影响。
  enabled?: boolean;
  onData?: (data: Uint8Array) => void;
  onExit?: (info: { code: number; reason: Reason; msg?: string }) => void;
}

export function useTerminal(args: UseTerminalArgs) {
  const [state, setState] = useState<TerminalState>("opening");

  const dataEvent = `terminal:${args.terminalID}:data`;
  const exitEvent = `terminal:${args.terminalID}:exit`;

  useEffect(() => {
    // attach 模式下完全不注册监听 / 不开 PTY,清理也不 TerminalClose,
    // 避免与 F6 store 这个唯一的 Wails 订阅者互删监听。
    if (args.enabled === false) return;
    let cancelled = false;

    EventsOn(dataEvent, (payload: { data: string }) => {
      args.onData?.(base64ToBytes(payload.data));
    });
    EventsOn(
      exitEvent,
      (payload: { code: number; reason: Reason; msg?: string }) => {
        args.onExit?.(payload);
        setState("idle");
        EventsOff(dataEvent);
        EventsOff(exitEvent);
      },
    );

    App.TerminalOpen(
      args.terminalID,
      args.projectId,
      args.deviceId,
      args.cols,
      args.rows,
    ).then(
      () => {
        if (cancelled) {
          App.TerminalClose(args.terminalID);
          return;
        }
        setState("open");
      },
      (err) => {
        if (!cancelled) {
          setState("idle");
          args.onExit?.({ code: -1, reason: "error", msg: String(err) });
        }
        EventsOff(dataEvent);
        EventsOff(exitEvent);
      },
    );

    return () => {
      cancelled = true;
      EventsOff(dataEvent);
      EventsOff(exitEvent);
      App.TerminalClose(args.terminalID).catch(() => {});
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [args.terminalID]);

  const write = useCallback(
    (data: string) => App.TerminalWrite(args.terminalID, data),
    [args.terminalID],
  );

  const resize = useCallback(
    (cols: number, rows: number) =>
      App.TerminalResize(args.terminalID, cols, rows),
    [args.terminalID],
  );

  return { state, write, resize };
}
