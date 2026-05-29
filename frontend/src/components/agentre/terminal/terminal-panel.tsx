import { useEffect, useRef } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";

import { useTerminal } from "./use-terminal";

export interface TerminalPanelProps {
  sessionID: number;
}

export function TerminalPanel({ sessionID }: TerminalPanelProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const xtermRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);

  useEffect(() => {
    if (!containerRef.current) return;
    const term = new Terminal({
      fontFamily: "'JetBrains Mono', 'Menlo', 'Monaco', monospace",
      fontSize: 13,
      theme: { background: "#0b1220", foreground: "#e2e8f0" },
      scrollback: 500,
      cursorBlink: true,
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.loadAddon(new WebLinksAddon());
    term.open(containerRef.current);
    fit.fit();
    xtermRef.current = term;
    fitRef.current = fit;

    return () => {
      term.dispose();
      xtermRef.current = null;
      fitRef.current = null;
    };
  }, []);

  const { write, resize } = useTerminal({
    sessionID,
    cols: 80,
    rows: 24,
    onData: (data) => {
      xtermRef.current?.write(data);
    },
    onExit: () => {
      /* parent toggle store will unmount us */
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
    <div className="flex-1 min-h-0 bg-[#0b1220]" data-testid="terminal-panel">
      <div ref={containerRef} className="h-full w-full p-2" />
    </div>
  );
}
