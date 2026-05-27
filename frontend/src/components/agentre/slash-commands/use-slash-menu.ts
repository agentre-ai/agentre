import { useCallback, useEffect, useMemo, useState } from "react";

import type { Editor } from "@tiptap/react";

import {
  filterByQuery,
  listAvailable,
  type SlashCommand,
  type SlashExec,
} from "./registry";
import { detectSlashTrigger } from "./trigger";

// SlashMenuState 暴露给消费者 (chat-input) 的运行时状态。
// open=false 时 anchor/items/query 视作未定义,UI 不应渲染弹层。
export type SlashMenuState = {
  open: boolean;
  // 弹层锚定的视口坐标 (px),用 editor.view.coordsAtPos(triggerPos) 拿到。
  anchorRect: { left: number; top: number; bottom: number } | null;
  // 当前候选(按 query 过滤后)。
  items: SlashCommand[];
  // 用户当前高亮的下标;items 变化时会被 clamp 回 0..items.length-1。
  selectedIndex: number;
  query: string;
};

export type UseSlashMenuOpts = {
  // 编辑器实例;TipTap useEditor 返回的 Editor。null 时 hook noop。
  editor: Editor | null;
  // 当前会话的 backend 类型,决定可用命令清单。空串 hook 视作 disabled。
  backendType: string;
  // 用户选中命令时调用;literal_text 由 chat-input 直接把文本填回编辑器(不自动发送),
  // rpc 由 chat-input 转交给 ChatPanel 走 Wails 绑定。
  onSelect: (cmd: SlashCommand, exec: SlashExec) => void;
};

// useSlashMenu 监听 TipTap selection/update 事件,实时检测 slash trigger,维护
// 弹层 open / anchorRect / items / selectedIndex 状态;暴露键盘 navigation /
// confirm / close 给消费者(由 chat-input 在 handleKeyDown 里调用)。
//
// 返回:
//   state           当前弹层状态(open + 候选 + 高亮);UI 渲染入口。
//   onKeyDown       供 editorProps.handleKeyDown 调用;返回 true 表示已消化按键。
//   close           手动关闭弹层(选中命令后 chat-input 也会调一次兜底)。
export function useSlashMenu({
  editor,
  backendType,
  onSelect,
}: UseSlashMenuOpts): {
  state: SlashMenuState;
  onKeyDown: (event: KeyboardEvent) => boolean;
  // pick 是统一的"选中命令"入口,鼠标点击 + 键盘 Enter/Tab 都走这条。
  // 内部负责清掉 / + query 段、关弹层、回调 onSelect。
  pick: (cmd: SlashCommand) => void;
  // setSelectedIndex 让消费者(SlashPopover)在 mouseenter 时同步高亮态,
  // 避免鼠标和键盘两套高亮打架。
  setSelectedIndex: (idx: number) => void;
  close: () => void;
} {
  const [query, setQuery] = useState<string>("");
  const [open, setOpen] = useState(false);
  const [anchorRect, setAnchorRect] = useState<
    SlashMenuState["anchorRect"] | null
  >(null);
  const [selectedIndex, setSelectedIndex] = useState(0);

  const available = useMemo(() => listAvailable(backendType), [backendType]);
  const items = useMemo(
    () => filterByQuery(available, query),
    [available, query],
  );

  // items 变化时把 selectedIndex 拉回有效范围。
  useEffect(() => {
    if (selectedIndex >= items.length) {
      setSelectedIndex(items.length > 0 ? items.length - 1 : 0);
    }
  }, [items.length, selectedIndex]);

  const close = useCallback(() => {
    setOpen(false);
    setAnchorRect(null);
    setQuery("");
    setSelectedIndex(0);
  }, []);

  // 监听编辑器 update / selectionUpdate,刷新触发态。
  useEffect(() => {
    if (!editor) return;
    const recompute = () => {
      if (available.length === 0) {
        if (open) close();
        return;
      }
      const { state } = editor;
      const { $from, empty } = state.selection;
      if (!empty) {
        if (open) close();
        return;
      }
      const before = $from.parent.textBetween(0, $from.parentOffset);
      const hit = detectSlashTrigger(before);
      if (!hit) {
        if (open) close();
        return;
      }
      const triggerPos = $from.start() + hit.startOffset;
      let rect: SlashMenuState["anchorRect"];
      try {
        const c = editor.view.coordsAtPos(triggerPos);
        rect = { left: c.left, top: c.top, bottom: c.bottom };
      } catch {
        rect = null;
      }
      setQuery(hit.query);
      setAnchorRect(rect);
      setOpen(true);
    };
    editor.on("update", recompute);
    editor.on("selectionUpdate", recompute);
    return () => {
      editor.off("update", recompute);
      editor.off("selectionUpdate", recompute);
    };
  }, [editor, available, open, close]);

  // 选中命令:用 ProseMirror deleteRange 把 / + query 段从编辑器去掉,再回调 onSelect。
  // literal_text 由 chat-input 把完整命令文本填回编辑器(不自动发送);rpc 由 chat-input
  // 转交给上层调用 handler。这里只负责"清掉 trigger 文本 + 关弹层 + 通知 onSelect"。
  const confirm = useCallback(
    (cmd: SlashCommand) => {
      const exec = cmd.resolve(backendType);
      if (!exec) {
        close();
        return;
      }
      if (editor) {
        const { state } = editor;
        const { $from } = state.selection;
        const before = $from.parent.textBetween(0, $from.parentOffset);
        const hit = detectSlashTrigger(before);
        if (hit) {
          const from = $from.start() + hit.startOffset;
          const to = $from.pos;
          editor.chain().focus().deleteRange({ from, to }).run();
        }
      }
      close();
      onSelect(cmd, exec);
    },
    [backendType, close, editor, onSelect],
  );

  const onKeyDown = useCallback(
    (event: KeyboardEvent): boolean => {
      if (!open || items.length === 0) return false;
      switch (event.key) {
        case "ArrowDown":
          event.preventDefault();
          setSelectedIndex((i) => (i + 1) % items.length);
          return true;
        case "ArrowUp":
          event.preventDefault();
          setSelectedIndex((i) => (i - 1 + items.length) % items.length);
          return true;
        case "Enter":
        case "Tab": {
          event.preventDefault();
          const cmd = items[selectedIndex] ?? items[0];
          if (cmd) confirm(cmd);
          return true;
        }
        case "Escape":
          event.preventDefault();
          close();
          return true;
        default:
          return false;
      }
    },
    [open, items, selectedIndex, confirm, close],
  );

  const state: SlashMenuState = useMemo(
    () => ({ open, anchorRect, items, selectedIndex, query }),
    [open, anchorRect, items, selectedIndex, query],
  );

  return { state, onKeyDown, pick: confirm, setSelectedIndex, close };
}
