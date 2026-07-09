import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";
import { useTranslation } from "react-i18next";

import { useLocalCommandsStore } from "@/stores/local-commands-store";
import {
  TERMINAL_FONT_FAMILY,
  readTerminalTheme,
} from "../terminal/terminal-theme";
import {
  computeTerminalHeight,
  MIN_ROWS,
  MAX_ROWS,
  PADDING_PX,
  FALLBACK_CELL_PX,
} from "./terminal-height";

// 本地命令(`!cmd`)输出的只读展示。复用交互终端同一套 xterm 渲染:让 xterm 自己解释
// ANSI/OSC/控制序列(颜色、光标、标题序列…),而不是用正则剥转义 —— 后者会漏掉前导
// ESC 字节、OSC、裸 \r 等,在 <pre> 里渲染成乱码。数据源是 useLocalCommandsStore 里
// 该命令保留原始字节的 output 字符串(decode 只做 base64→utf8,不剥转义)。
//
// 只读:disableStdin + 不闪光标,不接 stdin、不 resize PTY(命令可能已结束,PTY 已退)。
// 懒挂载:卡片滚入视口才建 xterm —— 长 transcript 里每张卡都常驻一个 canvas 终端太重。
// 环境无 IntersectionObserver(happy-dom 测试 / SSR)时直接 eager,保证输出始终可见。
export function OutputTerminal({ terminalId }: { terminalId: string }) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const xtermRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const writtenLenRef = useRef(0);
  const [mounted, setMounted] = useState(false);
  const { t } = useTranslation();
  const cellHeightRef = useRef(FALLBACK_CELL_PX);
  const isEmptyFinished = useLocalCommandsStore((s) => {
    const e = s.entries[terminalId];
    return !!e && e.status !== "running" && e.output === "";
  });

  // 懒挂载门控。
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    if (typeof IntersectionObserver === "undefined") {
      setMounted(true);
      return;
    }
    const io = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting)) {
        setMounted(true);
        io.disconnect();
      }
    });
    io.observe(el);
    return () => io.disconnect();
  }, []);

  // 构建只读 xterm(layout effect:在写入前先建好实例)。
  useLayoutEffect(() => {
    if (!mounted || !containerRef.current) return;
    const term = new Terminal({
      fontFamily: TERMINAL_FONT_FAMILY,
      fontSize: 13,
      theme: readTerminalTheme(),
      scrollback: 1000,
      disableStdin: true,
      cursorBlink: false,
      cursorStyle: "bar",
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.loadAddon(new WebLinksAddon());
    term.open(containerRef.current);
    fit.fit();
    xtermRef.current = term;
    fitRef.current = fit;
    writtenLenRef.current = 0;
    return () => {
      term.dispose();
      xtermRef.current = null;
      fitRef.current = null;
    };
  }, [mounted]);

  // seed + 增量:写新增片段 + 每次按内容重算容器高度(自适应且封顶)。
  useEffect(() => {
    if (!mounted) return;
    const applyHeight = () => {
      const term = xtermRef.current;
      const el = containerRef.current;
      if (!term || !el) return;
      // 有真实布局时用它反推行高(容器高 - padding)/ 行数;happy-dom 下退回兜底常量。
      if (el.clientHeight > 0 && term.rows > 0) {
        const m = (el.clientHeight - PADDING_PX) / term.rows;
        if (Number.isFinite(m) && m > 0) cellHeightRef.current = m;
      }
      const b = term.buffer.active;
      const contentRows = b.baseY + b.cursorY + 1;
      el.style.height = `${computeTerminalHeight({
        contentRows,
        cellHeight: cellHeightRef.current,
        minRows: MIN_ROWS,
        maxRows: MAX_ROWS,
        paddingPx: PADDING_PX,
      })}px`;
      fitRef.current?.fit();
    };
    const writeDelta = () => {
      const entry = useLocalCommandsStore.getState().get(terminalId);
      const term = xtermRef.current;
      if (!entry || !term) return;
      if (entry.output.length > writtenLenRef.current) {
        term.write(entry.output.slice(writtenLenRef.current));
        writtenLenRef.current = entry.output.length;
      }
      applyHeight();
    };
    writeDelta(); // 首帧 seed + 定高。
    const unsub = useLocalCommandsStore.subscribe(writeDelta);
    return () => unsub();
  }, [mounted, terminalId]);

  // 容器宽度变化时重新 fit(只换列宽以正确回绕,不 resize PTY)。
  useEffect(() => {
    if (!mounted || !containerRef.current) return;
    const ro = new ResizeObserver(() => fitRef.current?.fit());
    ro.observe(containerRef.current);
    return () => ro.disconnect();
  }, [mounted]);

  // 跟随 app light/dark 切换重置终端主题(与交互终端一致)。
  useEffect(() => {
    if (!mounted || typeof document === "undefined") return;
    const apply = () => {
      const term = xtermRef.current;
      if (term) term.options.theme = readTerminalTheme();
    };
    apply();
    const obs = new MutationObserver(apply);
    obs.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class"],
    });
    return () => obs.disconnect();
  }, [mounted]);

  if (isEmptyFinished) {
    return (
      <div
        data-testid="local-command-terminal"
        className="bg-code-surface px-3 py-2 text-2xs text-muted-foreground"
      >
        {t("localCommand.noOutput")}
      </div>
    );
  }

  return (
    <div
      ref={containerRef}
      data-testid="local-command-terminal"
      className="w-full overflow-hidden bg-code-surface px-2 py-1.5"
    />
  );
}
