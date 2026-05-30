import { useEffect, useState, useCallback } from "react";
import * as App from "@/../wailsjs/go/app/App";
import { EventsOn, EventsOff } from "@/../wailsjs/runtime/runtime";

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
  onData?: (data: string) => void;
  onExit?: (info: { code: number; reason: Reason; msg?: string }) => void;
}

export function useTerminal(args: UseTerminalArgs) {
  const [state, setState] = useState<TerminalState>("opening");

  const dataEvent = `terminal:${args.terminalID}:data`;
  const exitEvent = `terminal:${args.terminalID}:exit`;

  useEffect(() => {
    let cancelled = false;

    EventsOn(dataEvent, (payload: { data: string }) => {
      args.onData?.(payload.data);
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
