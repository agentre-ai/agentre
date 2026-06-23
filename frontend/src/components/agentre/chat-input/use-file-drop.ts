import { useEffect, useRef, useState, type RefObject } from "react";

import { registerDropZone } from "@/lib/file-drop";

// useFileDropZone 把一个元素注册成 Wails 文件 drop 目标,并用 HTML5 拖拽事件驱动
// 高亮状态。真实落地路径来自原生 OnFileDrop(经路由层),HTML5 事件只负责:
//   - dragenter/dragleave → isDragOver 高亮(进入计数避免子元素抖动);
//   - dragover/drop preventDefault → 阻止 webview 把拖入文件当导航/打开。
export function useFileDropZone(opts: {
  ref: RefObject<HTMLElement | null>;
  enabled: boolean;
  onPaths: (paths: string[]) => void;
}): { isDragOver: boolean } {
  const { ref, enabled, onPaths } = opts;
  const [isDragOver, setIsDragOver] = useState(false);
  const onPathsRef = useRef(onPaths);
  useEffect(() => {
    onPathsRef.current = onPaths;
  }, [onPaths]);

  useEffect(() => {
    const el = ref.current;
    if (!enabled || !el) return;

    const unregister = registerDropZone(el, (paths) =>
      onPathsRef.current(paths),
    );

    let depth = 0;
    const onDragEnter = (e: DragEvent) => {
      if (!e.dataTransfer?.types.includes("Files")) return;
      depth++;
      setIsDragOver(true);
    };
    const onDragOver = (e: DragEvent) => e.preventDefault();
    const onDragLeave = () => {
      depth = Math.max(0, depth - 1);
      if (depth === 0) setIsDragOver(false);
    };
    const onDrop = (e: DragEvent) => {
      e.preventDefault();
      depth = 0;
      setIsDragOver(false);
    };

    el.addEventListener("dragenter", onDragEnter);
    el.addEventListener("dragover", onDragOver);
    el.addEventListener("dragleave", onDragLeave);
    el.addEventListener("drop", onDrop);
    return () => {
      unregister();
      el.removeEventListener("dragenter", onDragEnter);
      el.removeEventListener("dragover", onDragOver);
      el.removeEventListener("dragleave", onDragLeave);
      el.removeEventListener("drop", onDrop);
    };
  }, [ref, enabled]);

  return { isDragOver };
}
