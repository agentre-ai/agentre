import { useEffect, useState, useCallback } from "react";
import * as App from "@/../wailsjs/go/app/App";
import { EventsOn, EventsOff } from "@/../wailsjs/runtime/runtime";

// Terminal stdout arrives base64-encoded: raw PTY bytes survive the JSON event
// bridge that way (a UTF-8 string would have multibyte sequences split across
// chunks mangled to U+FFFD; see terminal_svc.pump). Decode to bytes and hand
// them to xterm, which reassembles split multibyte sequences across writes.
function base64ToBytes(b64: string): Uint8Array {
  const bin = atob(b64);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  return bytes;
}

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
  onData?: (data: Uint8Array) => void;
  onExit?: (info: { code: number; reason: Reason; msg?: string }) => void;
}

export function useTerminal(args: UseTerminalArgs) {
  const [state, setState] = useState<TerminalState>("opening");

  const dataEvent = `terminal:${args.terminalID}:data`;
  const exitEvent = `terminal:${args.terminalID}:exit`;

  useEffect(() => {
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
