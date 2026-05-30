import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import { useTranslation } from "react-i18next";
import { Terminal, type ITheme } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";
import { toast } from "sonner";

import { useTerminal } from "./use-terminal";
import { resolveTerminalTheme } from "./terminal-theme";
import { attachXtermRolloverGuard } from "./xterm-rollover-guard";

export interface TerminalPanelProps {
  terminalID: string;
  projectId: number;
  deviceId: string;
  active?: boolean;
  onClose: () => void;
}

// 平台等宽字体放最前，Nerd Font 仅作图标/powerline 兜底。把 Nerd Font 排在前面时，
// 若某个变体缺 Bold 字面，浏览器会对一行里部分字符合成 faux-bold → 出现粗细混杂的
// 马赛克；平台 mono(mac→Menlo / win→Consolas / linux→DejaVu)自带匹配的 Bold，放前面
// 可消除这个问题。CJK 字体兜底中文等宽。
const TERMINAL_FONT_FAMILY = [
  "Menlo",
  "Consolas",
  "'DejaVu Sans Mono'",
  "'JetBrainsMono NFM'",
  "'JetBrainsMono Nerd Font Mono'",
  "'JetBrains Mono'",
  "'Symbols Nerd Font Mono'",
  "'Noto Sans Mono CJK SC'",
  "monospace",
].join(", ");

// 跟随应用主题：用 .dark class 选 light/dark 调色板，并把应用实时的 --background/
// --foreground 叠加为终端底色，使终端与周围 UI 一致；完整 16 色 ANSI 由 resolveTerminalTheme
// 提供，避免只设 bg/fg 时浅色模式下 ANSI 亮色发白看不清。
function readTerminalTheme(): ITheme {
  if (typeof document === "undefined") {
    return resolveTerminalTheme(true);
  }
  const root = document.documentElement;
  const isDark = root.classList.contains("dark");
  const styles = getComputedStyle(root);
  const bg = styles.getPropertyValue("--background").trim();
  const fg = styles.getPropertyValue("--foreground").trim();
  return resolveTerminalTheme(isDark, bg, fg);
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

  const fitAndResize = useCallback(() => {
    const f = fitRef.current;
    const t = xtermRef.current;
    if (!f || !t) return;
    f.fit();
    void resize(t.cols, t.rows);
  }, [resize]);

  useLayoutEffect(() => {
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

    // Cmd+C/Ctrl+C: copy selection if any, else fall through to xterm SIGINT default.
    term.attachCustomKeyEventHandler((ev) => {
      // 在 IME composition / keyCode=229 时早返回，别吞掉应交给 IME / xterm 内部
      // 处理的按键，否则可能导致字符丢失 (W3C UI Events §5.4.3)。
      if (ev.isComposing || ev.keyCode === 229) return true;
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

  useLayoutEffect(() => {
    if (!active) return;
    xtermRef.current?.focus();
    if (state === "open") {
      fitAndResize();
    }
  }, [active, state, fitAndResize]);

  // Re-theme xterm when the app switches between light and dark mode.
  useEffect(() => {
    if (typeof document === "undefined") return;
    const applyTheme = () => {
      const term = xtermRef.current;
      if (!term) return;
      term.options.theme = readTerminalTheme();
    };
    // Apply once on mount. App toggles the `.dark` class in its own layout
    // effect; on a same-commit mount (a restored terminal tab on app startup)
    // that runs AFTER this terminal's xterm was constructed but BEFORE this
    // observer registers, so the MutationObserver never sees that initial class
    // and the terminal would stay light. This passive effect runs after all
    // layout effects, so re-reading here picks up the resolved theme.
    applyTheme();
    const observer = new MutationObserver(applyTheme);
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class"],
    });
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    const term = xtermRef.current;
    if (!term) return;
    const send = (d: string) => void write(d);
    const sub = term.onData(send);
    // IME 快速输入丢字符旁路：xterm 在 key-rollover 时会把中间字符当重复输入丢掉，
    // 这里在 textarea 的 input 事件上补发被丢的字符 (详见 xterm-rollover-guard.ts)。
    const guard = attachXtermRolloverGuard(term, send);
    return () => {
      sub.dispose();
      guard.dispose();
    };
  }, [write]);

  useEffect(() => {
    if (!containerRef.current) return;
    const ro = new ResizeObserver(() => {
      if (!active || state !== "open") return;
      fitAndResize();
    });
    ro.observe(containerRef.current);
    return () => ro.disconnect();
  }, [active, state, fitAndResize]);

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
