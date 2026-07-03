import { act, render, screen } from "@testing-library/react";
import type { RefObject } from "react";
import { describe, expect, it, vi } from "vitest";

import type { Editor } from "@tiptap/react";

// chat.tsx uses wailsjs runtime (OnFileDrop/OnFileDropOff via useFileDropZone)
vi.mock("../../../../wailsjs/runtime/runtime", async () => {
  const actual = await vi.importActual<
    typeof import("../../../../wailsjs/runtime/runtime")
  >("../../../../wailsjs/runtime/runtime");
  return {
    ...actual,
    OnFileDrop: vi.fn(),
    OnFileDropOff: vi.fn(),
  };
});

import { ChatComposer } from "../chat";

describe("ChatComposer command mode", () => {
  it("shows command-mode banner when input starts with !", async () => {
    const editorRef: RefObject<Editor | null> = { current: null };
    const onRunCommand = vi.fn();

    render(
      <ChatComposer
        editorRef={editorRef}
        onSubmit={() => undefined}
        onRunCommand={onRunCommand}
      />,
    );

    // Wait for the editor to mount
    await screen.findByRole("textbox");

    act(() => {
      editorRef.current!.commands.insertContent("!ls");
    });

    expect(screen.getByText(/命令模式|Command mode/)).toBeInTheDocument();
  });

  it("run button replaces send button in command mode", async () => {
    const editorRef: RefObject<Editor | null> = { current: null };

    render(
      <ChatComposer
        editorRef={editorRef}
        onSubmit={() => undefined}
        onRunCommand={vi.fn()}
      />,
    );

    await screen.findByRole("textbox");

    // Initially: normal Send button present
    expect(screen.getByRole("button", { name: "Send" })).toBeInTheDocument();

    act(() => {
      editorRef.current!.commands.insertContent("!echo hi");
    });

    // In command mode: Run button should be present, Send should not
    expect(
      screen.getByRole("button", { name: /运行命令|Run command/i }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Send" }),
    ).not.toBeInTheDocument();
  });

  it("banner disappears when leading ! is removed (cleared)", async () => {
    const editorRef: RefObject<Editor | null> = { current: null };

    render(
      <ChatComposer
        editorRef={editorRef}
        onSubmit={() => undefined}
        onRunCommand={vi.fn()}
      />,
    );

    await screen.findByRole("textbox");

    act(() => {
      editorRef.current!.commands.insertContent("!cmd");
    });
    expect(screen.getByText(/命令模式|Command mode/)).toBeInTheDocument();

    act(() => {
      editorRef.current!.commands.clearContent();
    });

    expect(screen.queryByText(/命令模式|Command mode/)).not.toBeInTheDocument();
  });
});
