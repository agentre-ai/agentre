import { act, render } from "@testing-library/react";
import { createRef, type RefObject } from "react";
import { describe, expect, it, vi } from "vitest";

import type { Editor } from "@tiptap/react";

import { AIChatInput } from "../index";
import type { AIChatInputHandle } from "../types";

/** 在编辑器的 contentEditable DOM 上派发一次 Enter keydown，触发 sendOnEnter 路径。 */
function pressEnter(editor: Editor) {
  const event = new KeyboardEvent("keydown", {
    key: "Enter",
    bubbles: true,
    cancelable: true,
  });
  editor.view.dom.dispatchEvent(event);
}

describe("AIChatInput command mode", () => {
  it("toggles command mode on leading ! and strips it on submit", () => {
    const onCommandModeChange = vi.fn();
    const onCommandSubmit = vi.fn();
    const onSubmit = vi.fn();
    const editorRef: RefObject<Editor | null> = { current: null };
    const handleRef = createRef<AIChatInputHandle>();

    render(
      <AIChatInput
        ref={handleRef}
        editorRef={editorRef}
        sendOnEnter
        onSubmit={onSubmit}
        onCommandModeChange={onCommandModeChange}
        onCommandSubmit={onCommandSubmit}
      />,
    );

    const editor = editorRef.current!;

    // Insert content starting with !
    act(() => {
      editor.commands.insertContent("!go test ./...");
    });

    expect(onCommandModeChange).toHaveBeenLastCalledWith(true);

    // Press Enter to submit
    act(() => {
      pressEnter(editor);
    });

    expect(onCommandSubmit).toHaveBeenCalledWith("go test ./...");
    expect(onSubmit).not.toHaveBeenCalled();
    // Editor should be cleared after submit
    expect(editor.getText()).toBe("");
  });

  it("stays normal and routes to onSubmit without leading !", () => {
    const onCommandSubmit = vi.fn();
    const onSubmit = vi.fn();
    const editorRef: RefObject<Editor | null> = { current: null };

    render(
      <AIChatInput
        editorRef={editorRef}
        sendOnEnter
        onSubmit={onSubmit}
        onCommandSubmit={onCommandSubmit}
      />,
    );

    const editor = editorRef.current!;

    act(() => {
      editor.commands.insertContent("hello");
    });

    act(() => {
      pressEnter(editor);
    });

    expect(onSubmit).toHaveBeenCalledWith("hello");
    expect(onCommandSubmit).not.toHaveBeenCalled();
  });

  it("does not call onCommandSubmit for bare ! (empty command)", () => {
    const onCommandSubmit = vi.fn();
    const onSubmit = vi.fn();
    const editorRef: RefObject<Editor | null> = { current: null };

    render(
      <AIChatInput
        editorRef={editorRef}
        sendOnEnter
        onSubmit={onSubmit}
        onCommandSubmit={onCommandSubmit}
      />,
    );

    const editor = editorRef.current!;

    act(() => {
      editor.commands.insertContent("!");
    });

    act(() => {
      pressEnter(editor);
    });

    expect(onCommandSubmit).not.toHaveBeenCalled();
    expect(onSubmit).not.toHaveBeenCalled();
    // Content should be cleared
    expect(editor.getText()).toBe("");
  });

  it("dedupes onCommandModeChange — does not re-fire when already in command mode", () => {
    const onCommandModeChange = vi.fn();
    const editorRef: RefObject<Editor | null> = { current: null };

    render(
      <AIChatInput
        editorRef={editorRef}
        sendOnEnter
        onSubmit={() => {}}
        onCommandModeChange={onCommandModeChange}
      />,
    );

    const editor = editorRef.current!;

    act(() => {
      editor.commands.insertContent("!a");
    });
    const callsAfterFirst = onCommandModeChange.mock.calls.length;

    act(() => {
      editor.commands.insertContent("b");
    });
    // should not have been called again since still in command mode
    expect(onCommandModeChange.mock.calls.length).toBe(callsAfterFirst);
    expect(onCommandModeChange).toHaveBeenLastCalledWith(true);
  });

  it("exits command mode when ! is removed", () => {
    const onCommandModeChange = vi.fn();
    const editorRef: RefObject<Editor | null> = { current: null };
    const handleRef = createRef<AIChatInputHandle>();

    render(
      <AIChatInput
        ref={handleRef}
        editorRef={editorRef}
        sendOnEnter
        onSubmit={() => {}}
        onCommandModeChange={onCommandModeChange}
      />,
    );

    const editor = editorRef.current!;

    act(() => {
      editor.commands.insertContent("!cmd");
    });
    expect(onCommandModeChange).toHaveBeenLastCalledWith(true);

    act(() => {
      handleRef.current!.clear();
    });

    // After clearing, should report false
    expect(onCommandModeChange).toHaveBeenLastCalledWith(false);
  });
});
