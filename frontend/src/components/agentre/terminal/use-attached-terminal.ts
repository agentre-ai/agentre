import { useCallback, useEffect, useRef } from "react";
import type { Terminal } from "@xterm/xterm";
import * as App from "@/../wailsjs/go/app/App";
import { useLocalCommandsStore } from "@/stores/local-commands-store";

// attach 模式数据源:不开 / 不关 PTY,不订阅 Wails terminal 事件(单一订阅者是
// F6 的 store)。从 useLocalCommandsStore 里取该 terminalId 的 output 做 seed,
// 订阅 store 增量写 xterm;stdin 走 App.TerminalWrite,resize 走 App.TerminalResize。
//
// 注意:增量输出依赖宿主会话 ChatPanel 里的 F6 store 订阅存活(它是唯一的
// Wails `terminal:<id>:data` 事件订阅者);若宿主 Tab 被关闭,store 停止更新,
// attach tab 的显示会停滞,但后端 PTY 仍在运行。这是单订阅者设计的固有行为。
export function useAttachedTerminal({
  terminalID,
  xtermRef,
  enabled,
}: {
  terminalID: string;
  xtermRef: React.RefObject<Terminal | null>;
  enabled: boolean;
}) {
  const writtenLenRef = useRef(0);
  useEffect(() => {
    if (!enabled) return;
    writtenLenRef.current = 0;
    const writeDelta = () => {
      const entry = useLocalCommandsStore.getState().get(terminalID);
      const term = xtermRef.current;
      if (!entry || !term) return;
      if (entry.output.length > writtenLenRef.current) {
        term.write(entry.output.slice(writtenLenRef.current));
        writtenLenRef.current = entry.output.length;
      }
    };
    writeDelta(); // 首帧 seed:xterm 已由先于本 effect 跑的 layout effect 创建。
    const unsub = useLocalCommandsStore.subscribe(writeDelta);
    return () => unsub();
  }, [enabled, terminalID, xtermRef]);

  const write = useCallback(
    (d: string) => App.TerminalWrite(terminalID, d),
    [terminalID],
  );
  const resize = useCallback(
    (c: number, r: number) => App.TerminalResize(terminalID, c, r),
    [terminalID],
  );
  // state 恒为 "open":PTY 已在跑,TerminalPanel 的 fit/resize effect(gated on
  // state==="open")才会执行。
  return { state: "open" as const, write, resize };
}
