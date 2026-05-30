import { useEffect, useRef, useState, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";
import { toast } from "sonner";

import { useTerminal } from "./use-terminal";

export interface TerminalPanelProps {
  terminalID: string;
  projectId: number;
  deviceId: string;
  active?: boolean;
  onClose: () => void;
}

const TERMINAL_FONT_FAMILY = [
  "'JetBrainsMono Nerd Font'",
  "'JetBrainsMono Nerd Font Mono'",
  "'JetBrains Mono NL'",
  "'JetBrains Mono'",
  "'MesloLGS NF'",
  "'Symbols Nerd Font Mono'",
  "'Noto Sans Mono CJK SC'",
  "'Menlo'",
  "'Monaco'",
  "monospace",
].join(", ");

function readTerminalTheme(): { background: string; foreground: string } {
  if (typeof document === "undefined") {
    return { background: "#17191c", foreground: "#e6e8eb" };
  }
  const root = document.documentElement;
  const styles = getComputedStyle(root);
  const bg = styles.getPropertyValue("--background").trim() || "#17191c";
  const fg = styles.getPropertyValue("--foreground").trim() || "#e6e8eb";
  return { background: bg, foreground: fg };
}

export function TerminalPanel({
  terminalID,
  projectId,
  deviceId,
  active = true,
  onClose,
}: TerminalPanelProps) {
  const { t } = useTranslation();
  const containerRef = useRef<HTMLDivElement | null>(null);
  const xtermRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const [connectionLost, setConnectionLost] = useState(false);

  // Stable dismiss for the banner — calls onClose, which unmounts us.
  const dismissAndClose = useCallback(() => {
    setConnectionLost(false);
    onClose();
  }, [onClose]);

  useEffect(() => {
    if (!containerRef.current) return;
    const term = new Terminal({
      fontFamily: TERMINAL_FONT_FAMILY,
      fontSize: 13,
      theme: readTerminalTheme(),
      scrollback: 500,
      cursorBlink: true,
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.loadAddon(new WebLinksAddon());
    term.open(containerRef.current);
    fit.fit();

    // Cmd+C/Ctrl+C: copy selection if any, else fall through to xterm SIGINT default.
    term.attachCustomKeyEventHandler((ev) => {
      if (ev.type !== "keydown") return true;
      const isCopyCombo =
        (ev.ctrlKey || ev.metaKey) &&
        !ev.shiftKey &&
        !ev.altKey &&
        (ev.key === "c" || ev.key === "C");
      if (!isCopyCombo) return true;
      const selection = term.getSelection();
      if (!selection) return true; // no selection → let xterm send SIGINT
      // Has selection → copy + swallow event so SIGINT does not fire.
      void navigator.clipboard.writeText(selection).catch(() => {
        // Clipboard permission may be blocked; intentionally silent.
      });
      return false;
    });

    xtermRef.current = term;
    fitRef.current = fit;

    return () => {
      term.dispose();
      xtermRef.current = null;
      fitRef.current = null;
    };
  }, []);

  useEffect(() => {
    if (!active) return;
    const id = window.setTimeout(() => {
      xtermRef.current?.focus();
    }, 0);
    return () => window.clearTimeout(id);
  }, [active]);

  // Re-theme xterm when the app switches between light and dark mode.
  // jsdom does not implement getComputedStyle for CSS custom properties, so
  // this effect is verified manually (Task 30) rather than via a DOM assertion.
  useEffect(() => {
    if (typeof document === "undefined") return;
    const applyTheme = () => {
      const term = xtermRef.current;
      if (!term) return;
      term.options.theme = readTerminalTheme();
    };
    const observer = new MutationObserver(applyTheme);
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class"],
    });
    return () => observer.disconnect();
  }, []);

  const { state, write, resize } = useTerminal({
    terminalID,
    projectId,
    deviceId,
    cols: 80,
    rows: 24,
    onData: (data) => {
      xtermRef.current?.write(data);
    },
    onExit: (info) => {
      if (info.reason === "connection_lost") {
        setConnectionLost(true);
        toast.error(t("terminal.toast.connectionLost"));
        return; // banner is shown; user dismisses to close.
      }
      if (info.reason === "error") {
        toast.error(
          t("terminal.toast.startFailed", {
            message: info.msg ?? t("terminal.unknown"),
          }),
        );
        onClose();
        return;
      }
      if (info.reason === "daemon_shutdown") {
        toast.warning(t("terminal.toast.agentredClosed"));
        onClose();
        return;
      }
      if (info.reason === "natural" && info.code !== 0) {
        toast.warning(t("terminal.toast.shellExited", { code: info.code }));
      }
      // natural code=0 or killed → silent close.
      onClose();
    },
  });

  useEffect(() => {
    const term = xtermRef.current;
    if (!term) return;
    const sub = term.onData((d) => {
      void write(d);
    });
    return () => sub.dispose();
  }, [write]);

  // Once the PTY is confirmed open, push the real fitted size. Open is issued
  // with placeholder dimensions, and the ResizeObserver-driven resize can land
  // before the backend handle is registered (dropped as "terminal closed"), so
  // without this the shell can stay stuck at the initial open size.
  useEffect(() => {
    if (state !== "open") return;
    const f = fitRef.current;
    const t = xtermRef.current;
    if (!f || !t) return;
    f.fit();
    void resize(t.cols, t.rows);
  }, [state, resize]);

  useEffect(() => {
    if (!containerRef.current) return;
    const ro = new ResizeObserver(() => {
      const f = fitRef.current;
      const t = xtermRef.current;
      if (!f || !t) return;
      f.fit();
      void resize(t.cols, t.rows);
    });
    ro.observe(containerRef.current);
    return () => ro.disconnect();
  }, [resize]);

  return (
    <div
      className="flex flex-1 min-h-0 flex-col bg-background"
      data-testid="terminal-panel"
    >
      {connectionLost ? (
        <div
          role="alert"
          className="flex items-center justify-between border-b border-red-700 bg-red-950/60 px-3 py-2 text-xs text-red-100"
        >
          <span>{t("terminal.banner.connectionLost")}</span>
          <button
            type="button"
            onClick={dismissAndClose}
            className="rounded border border-red-700 px-2 py-0.5 hover:bg-red-900"
          >
            {t("common.close")}
          </button>
        </div>
      ) : null}
      <div ref={containerRef} className="h-full w-full p-2" />
    </div>
  );
}
