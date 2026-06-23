import { OnFileDrop, OnFileDropOff } from "../../wailsjs/runtime/runtime";

type DropHandler = (paths: string[]) => void;

// 每窗口唯一的 OnFileDrop 回调 + 多个 composer 的 drop 目标。用 elementFromPoint
// 把 drop 路由到光标命中的 zone。
const registry = new Map<HTMLElement, DropHandler>();
let listening = false;

function handleDrop(x: number, y: number, paths: string[]): void {
  if (paths.length === 0) return;
  const target = document.elementFromPoint(x, y);
  if (!target) return;
  for (const [el, handler] of registry) {
    if (el === target || el.contains(target)) {
      handler(paths);
      return;
    }
  }
}

function ensureListening(): void {
  if (listening) return;
  // useDropTarget=true:Wails 只在带 --wails-drop-target:drop 的元素上空触发。
  OnFileDrop((x, y, paths) => handleDrop(x, y, paths), true);
  listening = true;
}

// registerDropZone 注册 composer 的 drop 目标 + 回调,并打上 Wails useDropTarget
// 所需的 CSS 标记。返回注销函数(最后一个注销时卸掉全局监听)。
export function registerDropZone(
  el: HTMLElement,
  handler: DropHandler,
): () => void {
  registry.set(el, handler);
  el.style.setProperty("--wails-drop-target", "drop");
  ensureListening();
  return () => {
    registry.delete(el);
    el.style.removeProperty("--wails-drop-target");
    if (registry.size === 0 && listening) {
      OnFileDropOff();
      listening = false;
    }
  };
}

// 仅测试用:重置 module 级单例状态。
export function __resetDropRegistryForTest(): void {
  registry.clear();
  listening = false;
}
