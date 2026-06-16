import {
  forwardRef,
  memo,
  useCallback,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  type RefObject,
} from "react";

import Document from "@tiptap/extension-document";
import HardBreak from "@tiptap/extension-hard-break";
import Paragraph from "@tiptap/extension-paragraph";
import Text from "@tiptap/extension-text";
import { Placeholder, UndoRedo } from "@tiptap/extensions";
import { EditorContent, useEditor, type Editor } from "@tiptap/react";

import { cn } from "@/lib/utils";

import {
  listAvailable,
  SlashHighlight,
  SlashPopover,
  useSlashMenu,
  type SlashCommand,
  type SlashExec,
} from "../slash-commands";

import { extractPlainText } from "./content";
import {
  applyInputHistoryMessage,
  getInputHistoryNavigationState,
  shouldContinueInputHistory,
  shouldIgnoreEditorShortcut,
  shouldStartInputHistory,
} from "./keyboard";
import type { AIChatInputHandle, ProseMirrorLikeNode } from "./types";

export type { AIChatInputDraft, AIChatInputHandle } from "./types";

export interface AIChatInputProps {
  onSubmit: (content: string) => void;
  onEmptyChange?: (empty: boolean) => void;
  sendOnEnter?: boolean;
  userMessageHistory?: string[];
  placeholder?: string;
  disabled?: boolean;
  className?: string;
  /** 编辑器创建时是否自动 focus。透传给 TipTap 的 `autofocus` 选项 ——
   *  让 TipTap 自己在 view 挂好之后再 focus，避开 mount 时 view 还没附加到 DOM
   *  的时序问题。新建会话场景下由 chat-panel 传 true。 */
  autoFocus?: boolean;
  /** 仅用于测试：暴露 TipTap editor 以便测试代码直接操作富文本。 */
  editorRef?: RefObject<Editor | null>;
  /** 当前会话 backend 类型 (claudecode/codex/builtin)。slash menu 据此过滤可用命令;
   *  空串或省略 → 不启用 slash menu。 */
  backendType?: string;
  /** 用户在 slash menu 选中一项时触发。literal_text 由 AIChatInput 内部直接把
   *  命令文本填回编辑器(不自动发送,等用户回车),所以父组件只需要处理 rpc 类。
   *  省略则 slash menu 不启用(等价于没 backend)。 */
  onSlashSelect?: (cmd: SlashCommand, exec: SlashExec) => void;
}

const AIChatInputComponent = forwardRef<AIChatInputHandle, AIChatInputProps>(
  function AIChatInput(
    {
      onSubmit,
      onEmptyChange,
      sendOnEnter = true,
      userMessageHistory = [],
      placeholder,
      disabled,
      className,
      autoFocus = false,
      editorRef,
      backendType,
      onSlashSelect,
    },
    ref,
  ) {
    const submitRef = useRef(onSubmit);
    const sendOnEnterRef = useRef(sendOnEnter);
    const onEmptyChangeRef = useRef(onEmptyChange);
    const historyRef = useRef(userMessageHistory);
    const historyIndexRef = useRef(-1);
    const applyingHistoryRef = useRef(false);
    const lastIsEmptyRef = useRef<boolean | null>(null);
    const triggerSubmitRef = useRef<() => void>(() => {});
    const slashKeyDownRef = useRef<(e: KeyboardEvent) => boolean>(() => false);
    const slashSelectRef = useRef(onSlashSelect);
    useEffect(() => {
      slashSelectRef.current = onSlashSelect;
    }, [onSlashSelect]);

    useEffect(() => {
      submitRef.current = onSubmit;
    }, [onSubmit]);

    useEffect(() => {
      sendOnEnterRef.current = sendOnEnter;
    }, [sendOnEnter]);

    useEffect(() => {
      onEmptyChangeRef.current = onEmptyChange;
    }, [onEmptyChange]);

    useEffect(() => {
      historyRef.current = userMessageHistory;
      historyIndexRef.current = -1;
    }, [userMessageHistory]);

    // 当前 backend 下合法的命令名集合 —— 喂给 SlashHighlight extension 做高亮。
    // 用 ref 持有最新值,extension 通过闭包 getter 读取,避免 backendType 变化时
    // 重建 editor。变化后由下面的 useEffect 派发 setSlashHighlightRefresh 让
    // plugin 重算 decoration。
    const validNames = useMemo(() => {
      if (!backendType) return new Set<string>();
      return new Set(listAvailable(backendType).map((c) => c.name));
    }, [backendType]);
    const validNamesRef = useRef(validNames);

    const editor = useEditor({
      autofocus: autoFocus ? "end" : false,
      extensions: [
        Document,
        HardBreak,
        Paragraph,
        Text,
        // 撤销/重做历史:ProseMirror 接管 contentEditable 后浏览器原生 Cmd/Ctrl+Z
        // 会失效,必须由该扩展提供 history 栈 + Mod-z/Mod-y/Shift-Mod-z 快捷键。
        UndoRedo,
        Placeholder.configure({ placeholder: placeholder || "" }),
        SlashHighlight.configure({
          getValidNames: () => validNamesRef.current,
        }),
      ],
      editorProps: {
        attributes: {
          class: cn(
            "ProseMirror min-h-10 max-h-[25vh] overflow-y-auto text-sm outline-none resize-none",
            className,
          ),
          role: "textbox",
          // 关掉浏览器的自动纠正 / 大写 / 拼写检查，避免和 IME 冲突。
          autocapitalize: "off",
          autocomplete: "off",
          autocorrect: "off",
          spellcheck: "false",
        },
        handleKeyDown: (view, event) => {
          if (!editor) return false;
          // 组词中（包括 keyCode===229 的兜底）一律放行给浏览器，
          // 避免 IME 候选回车被当成消息发送。
          if (shouldIgnoreEditorShortcut(view, event)) return false;
          // slash menu 打开时拦截 Up/Down/Enter/Tab/Esc;关闭时透明。
          if (slashKeyDownRef.current(event)) return true;

          const shouldSendOnEnter = sendOnEnterRef.current;
          const isEnter = event.key === "Enter";
          const mod = event.ctrlKey || event.metaKey;

          if (
            (event.key === "ArrowUp" || event.key === "ArrowDown") &&
            !event.altKey &&
            !event.ctrlKey &&
            !event.metaKey &&
            !event.shiftKey
          ) {
            const currentContent = extractPlainText(
              editor.state.doc as unknown as ProseMirrorLikeNode,
            );
            const nextHistoryState = getInputHistoryNavigationState({
              direction: event.key === "ArrowUp" ? "up" : "down",
              currentText: currentContent,
              historyIndex: historyIndexRef.current,
              userMessageHistory: historyRef.current,
              canStartHistory: shouldStartInputHistory(editor),
              canContinueHistory: shouldContinueInputHistory(editor),
            });

            if (nextHistoryState) {
              event.preventDefault();
              historyIndexRef.current = nextHistoryState.nextHistoryIndex;
              applyingHistoryRef.current = true;
              applyInputHistoryMessage(editor, nextHistoryState.nextMessage);
              return true;
            }
          }

          if (isEnter && shouldSendOnEnter && !event.shiftKey && !mod) {
            event.preventDefault();
            triggerSubmitRef.current();
            return true;
          }
          if (isEnter && !shouldSendOnEnter && mod) {
            event.preventDefault();
            triggerSubmitRef.current();
            return true;
          }
          return false;
        },
      },
      onUpdate: ({ editor: ed }) => {
        if (applyingHistoryRef.current) {
          applyingHistoryRef.current = false;
        } else {
          historyIndexRef.current = -1;
        }

        const isEmpty = ed.isEmpty;
        if (lastIsEmptyRef.current !== isEmpty) {
          lastIsEmptyRef.current = isEmpty;
          onEmptyChangeRef.current?.(isEmpty);
        }
      },
      editable: !disabled,
    });

    useEffect(() => {
      if (editorRef) editorRef.current = editor ?? null;
      return () => {
        if (editorRef) editorRef.current = null;
      };
    }, [editor, editorRef]);

    useEffect(() => {
      triggerSubmitRef.current = () => {
        if (!editor || disabled || editor.view.composing || editor.isEmpty)
          return;
        const content = extractPlainText(
          editor.state.doc as unknown as ProseMirrorLikeNode,
        );
        if (!content.trim()) return;

        historyIndexRef.current = -1;
        submitRef.current(content);
        editor.commands.clearContent(true);
        // 走 button 点击发送时，浏览器会把焦点抓到按钮上；clearContent 不会重新聚焦，
        // 这里显式 focus 回编辑器，保证用户能连续敲下一条消息而不用手再点一次输入框。
        // Enter 路径下原本就是聚焦态，再调用一次是无副作用的幂等操作。
        editor.commands.focus();
      };
    }, [disabled, editor]);

    useEffect(() => {
      editor?.setEditable(!disabled);
    }, [editor, disabled]);

    // backendType 变化时,新的 validNames 已经写进 ref,但 ProseMirror plugin 只在
    // doc 变化或显式 meta 时重算 decoration —— 这里主动触发一次让旧文本立刻按
    // 新规则重新染色(例:claudecode → builtin 后 /compact 应该退回默认色)。
    useEffect(() => {
      validNamesRef.current = validNames;
      editor?.commands.setSlashHighlightRefresh();
    }, [editor, validNames]);

    useImperativeHandle(
      ref,
      () => ({
        focus: () => editor?.commands.focus(),
        clear: () => {
          historyIndexRef.current = -1;
          editor?.commands.clearContent(true);
        },
        isEmpty: () => editor?.isEmpty ?? true,
        submit: () => triggerSubmitRef.current(),
        loadDraft: (draft) => {
          if (!editor) return;
          historyIndexRef.current = -1;
          applyInputHistoryMessage(editor, draft);
        },
      }),
      [editor],
    );

    // ── slash command menu 集成 ─────────────────────────────────────────────
    // 只在 backendType + onSlashSelect 同时具备时启用。useSlashMenu 监听 editor
    // selectionUpdate/update,实时检测触发位置;onKeyDown 同步给上面 handleKeyDown
    // 拦截 Up/Down/Enter/Tab/Esc。pick 统一鼠标 + 键盘选中入口。
    const slashSelectHandler = useCallback(
      (cmd: SlashCommand, exec: SlashExec) => {
        if (exec.kind === "literal_text") {
          // 只把命令文本填回输入框,不自动发送 —— 斜杠菜单是"智能提示",
          // 用户随时可以继续编辑(追加参数、删掉、改成普通消息)再决定是否回车发送。
          // 末尾补一个空格,既给参数留位置,也让 detectSlashTrigger 因 query 含空白而返回 null,
          // 避免插入完成后 popover 立刻基于新文本(如 /compact)重新弹出。
          if (editor) {
            editor.chain().focus().insertContent(`${exec.text} `).run();
          }
          return;
        }
        slashSelectRef.current?.(cmd, exec);
      },
      [editor],
    );
    const slashEnabled = !!(backendType && onSlashSelect);
    const slashMenu = useSlashMenu({
      editor: slashEnabled ? (editor ?? null) : null,
      backendType: backendType ?? "",
      onSelect: slashSelectHandler,
    });
    useEffect(() => {
      slashKeyDownRef.current = slashMenu.onKeyDown;
    }, [slashMenu.onKeyDown]);

    return (
      <>
        <EditorContent editor={editor} />
        {slashEnabled ? (
          <SlashPopover
            state={slashMenu.state}
            onPick={slashMenu.pick}
            onHover={slashMenu.setSelectedIndex}
          />
        ) : null}
      </>
    );
  },
);

export const AIChatInput = memo(
  AIChatInputComponent,
) as typeof AIChatInputComponent;
