import { useCallback, useLayoutEffect, useRef, useState } from "react";

import type { Editor } from "@tiptap/react";

import { normalizeSuggestionQuery } from "@/lib/suggestion-score";
import { localCommandHistoryStore } from "@/stores/local-command-history-store";

import { extractPlainText } from "../content";
import type { ProseMirrorLikeNode, TipTapDocNode } from "../types";
import { rankLocalCommandHistory } from "./rank";
import type {
  LocalCommandHistoryEntry,
  LocalCommandHistoryMenuState,
  LocalCommandHistoryScope,
} from "./types";

function closedState(query = ""): LocalCommandHistoryMenuState {
  return {
    open: false,
    anchorRect: null,
    items: [],
    selectedIndex: 0,
    query,
  };
}

function commandQuery(
  text: string,
): { bangIndex: number; query: string } | null {
  const leadingWhitespace = text.length - text.trimStart().length;
  if (text[leadingWhitespace] !== "!") return null;
  return {
    bangIndex: leadingWhitespace,
    query: text.slice(leadingWhitespace + 1),
  };
}

function plainTextDoc(content: string): TipTapDocNode {
  return {
    type: "doc",
    content: content
      .split("\n")
      .map((line) =>
        line
          ? { type: "paragraph", content: [{ type: "text", text: line }] }
          : { type: "paragraph" },
      ),
  };
}

function sameRect(
  left: LocalCommandHistoryMenuState["anchorRect"],
  right: LocalCommandHistoryMenuState["anchorRect"],
): boolean {
  if (left === right) return true;
  if (!left || !right) return false;
  return (
    left.left === right.left &&
    left.top === right.top &&
    left.bottom === right.bottom
  );
}

export function useLocalCommandHistoryMenu({
  editor,
  scope,
}: {
  editor: Editor | null;
  scope?: LocalCommandHistoryScope;
}): {
  state: LocalCommandHistoryMenuState;
  onKeyDown: (event: KeyboardEvent) => boolean;
  pick: (entry: LocalCommandHistoryEntry) => void;
  setSelectedIndex: (index: number) => void;
  clear: () => void;
} {
  const [state, setState] = useState<LocalCommandHistoryMenuState>(() =>
    closedState(),
  );
  const suppressedQueryRef = useRef<string | null>(null);
  const deviceId = scope?.deviceId;
  const cwd = scope?.cwd;

  useLayoutEffect(() => {
    suppressedQueryRef.current = null;
    if (!editor || deviceId === undefined || cwd === undefined) {
      setState(closedState());
      return;
    }
    const activeScope: LocalCommandHistoryScope = { deviceId, cwd };

    const recompute = () => {
      const { $from, empty } = editor.state.selection;
      if (!empty) {
        setState(closedState());
        return;
      }

      const text = extractPlainText(
        editor.state.doc as unknown as ProseMirrorLikeNode,
      );
      const hit = commandQuery(text);
      if (!hit) {
        suppressedQueryRef.current = null;
        setState(closedState());
        return;
      }

      if (suppressedQueryRef.current === hit.query) {
        setState(closedState(hit.query));
        return;
      }
      if (suppressedQueryRef.current !== null) {
        suppressedQueryRef.current = null;
      }

      const items = rankLocalCommandHistory(
        localCommandHistoryStore.list(activeScope),
        hit.query,
      );
      if (items.length === 0) {
        setState(closedState(hit.query));
        return;
      }

      let anchorRect: LocalCommandHistoryMenuState["anchorRect"] = null;
      try {
        const rect = editor.view.coordsAtPos($from.pos);
        anchorRect = {
          left: rect.left,
          top: rect.top,
          bottom: rect.bottom,
        };
      } catch {
        anchorRect = null;
      }

      setState((previous) => {
        const queryChanged =
          normalizeSuggestionQuery(previous.query) !==
          normalizeSuggestionQuery(hit.query);
        const selectedIndex = queryChanged
          ? 0
          : Math.min(previous.selectedIndex, items.length - 1);
        if (
          previous.open &&
          previous.query === hit.query &&
          previous.selectedIndex === selectedIndex &&
          sameRect(previous.anchorRect, anchorRect) &&
          previous.items.length === items.length &&
          previous.items.every(
            (entry, index) =>
              entry.command === items[index]?.command &&
              entry.lastUsedAt === items[index]?.lastUsedAt,
          )
        ) {
          return previous;
        }
        return {
          open: true,
          anchorRect,
          items,
          selectedIndex,
          query: hit.query,
        };
      });
    };

    editor.on("update", recompute);
    editor.on("selectionUpdate", recompute);
    recompute();
    return () => {
      editor.off("update", recompute);
      editor.off("selectionUpdate", recompute);
    };
  }, [cwd, deviceId, editor]);

  const dismiss = useCallback((query: string) => {
    suppressedQueryRef.current = query;
    setState(closedState(query));
  }, []);

  const pick = useCallback(
    (entry: LocalCommandHistoryEntry) => {
      if (!editor) return;
      const text = extractPlainText(
        editor.state.doc as unknown as ProseMirrorLikeNode,
      );
      const hit = commandQuery(text);
      if (!hit) {
        setState(closedState());
        return;
      }

      const nextText = `${text.slice(0, hit.bangIndex + 1)}${entry.command}`;
      dismiss(entry.command);
      editor.commands.setContent(plainTextDoc(nextText));
      editor.commands.focus("end");
    },
    [dismiss, editor],
  );

  const setSelectedIndex = useCallback((index: number) => {
    setState((previous) => {
      if (previous.items.length === 0) return previous;
      const selectedIndex = Math.max(
        0,
        Math.min(index, previous.items.length - 1),
      );
      return selectedIndex === previous.selectedIndex
        ? previous
        : { ...previous, selectedIndex };
    });
  }, []);

  const clear = useCallback(() => {
    if (deviceId === undefined || cwd === undefined) return;
    localCommandHistoryStore.clear({ deviceId, cwd });
    dismiss(state.query);
  }, [cwd, deviceId, dismiss, state.query]);

  const onKeyDown = useCallback(
    (event: KeyboardEvent): boolean => {
      if (!state.open || state.items.length === 0) return false;
      switch (event.key) {
        case "ArrowDown":
          event.preventDefault();
          setSelectedIndex((state.selectedIndex + 1) % state.items.length);
          return true;
        case "ArrowUp":
          event.preventDefault();
          setSelectedIndex(
            (state.selectedIndex - 1 + state.items.length) % state.items.length,
          );
          return true;
        case "Enter":
        case "Tab": {
          event.preventDefault();
          const entry = state.items[state.selectedIndex] ?? state.items[0];
          if (entry) pick(entry);
          return true;
        }
        case "Escape":
          event.preventDefault();
          dismiss(state.query);
          return true;
        default:
          return false;
      }
    },
    [dismiss, pick, setSelectedIndex, state],
  );

  return { state, onKeyDown, pick, setSelectedIndex, clear };
}
