import { act, render, screen } from "@testing-library/react";
import type { RefObject } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Editor } from "@tiptap/react";

import type { LocalCommandHistoryScope } from "@/components/agentre/chat-input/local-command-history/types";
import { localCommandHistoryStore } from "@/stores/local-command-history-store";

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

const historyScope: LocalCommandHistoryScope = {
  deviceId: "composer-device",
  cwd: "/composer/repo",
};
const newRemoteProjectScope: LocalCommandHistoryScope = {
  deviceId: "7",
  cwd: "/local/repo",
};
const resolvedRemoteProjectScope: LocalCommandHistoryScope = {
  deviceId: "7",
  cwd: "/home/me/proj",
};

beforeEach(() => {
  vi.restoreAllMocks();
  localCommandHistoryStore.clear(historyScope);
  localCommandHistoryStore.clear(newRemoteProjectScope);
  localCommandHistoryStore.clear(resolvedRemoteProjectScope);
});

function pressEnter(editor: Editor) {
  editor.view.dom.dispatchEvent(
    new KeyboardEvent("keydown", {
      key: "Enter",
      bubbles: true,
      cancelable: true,
    }),
  );
}

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

  it("Given an execution scope, When ! mode opens, Then ChatComposer passes that scope to the history menu", async () => {
    localCommandHistoryStore.record(historyScope, "pnpm test", 10);
    const editorRef: RefObject<Editor | null> = { current: null };

    render(
      <ChatComposer
        editorRef={editorRef}
        localCommandHistoryScope={historyScope}
        onSubmit={() => undefined}
        onRunCommand={vi.fn()}
      />,
    );

    await screen.findByRole("textbox");
    act(() => {
      editorRef.current!.commands.insertContent("!");
    });

    expect(
      await screen.findByRole("option", { name: "pnpm test" }),
    ).toBeInTheDocument();
  });

  it("Given a new remote project chat, When command execution resolves its scope, Then history records the resolved execution cwd instead of the local project path", async () => {
    const editorRef: RefObject<Editor | null> = { current: null };
    const onRunCommand = vi.fn().mockResolvedValue(resolvedRemoteProjectScope);

    render(
      <ChatComposer
        editorRef={editorRef}
        localCommandHistoryScope={newRemoteProjectScope}
        onSubmit={() => undefined}
        onRunCommand={onRunCommand}
      />,
    );

    await screen.findByRole("textbox");
    act(() => {
      editorRef.current!.commands.insertContent("!pwd");
      pressEnter(editorRef.current!);
    });

    expect(onRunCommand).toHaveBeenCalledWith("pwd");
    await vi.waitFor(() => {
      expect(localCommandHistoryStore.list(resolvedRemoteProjectScope)).toEqual(
        [expect.objectContaining({ command: "pwd" })],
      );
    });
    expect(localCommandHistoryStore.list(newRemoteProjectScope)).toEqual([]);
  });

  it("Given history persistence fails, When a command is submitted, Then execution still starts", async () => {
    const editorRef: RefObject<Editor | null> = { current: null };
    const submittedAt = 1_000;
    vi.spyOn(Date, "now").mockReturnValue(submittedAt);
    const onRunCommand = vi.fn().mockReturnValue(resolvedRemoteProjectScope);
    const recordSpy = vi
      .spyOn(localCommandHistoryStore, "record")
      .mockImplementation(() => {
        throw new Error("storage failed");
      });

    render(
      <ChatComposer
        editorRef={editorRef}
        localCommandHistoryScope={newRemoteProjectScope}
        onSubmit={() => undefined}
        onRunCommand={onRunCommand}
      />,
    );

    await screen.findByRole("textbox");
    act(() => {
      editorRef.current!.commands.insertContent("!pwd");
      pressEnter(editorRef.current!);
    });

    expect(onRunCommand).toHaveBeenCalledWith("pwd");
    await vi.waitFor(() => {
      expect(recordSpy).toHaveBeenCalledWith(
        resolvedRemoteProjectScope,
        "pwd",
        submittedAt,
      );
    });
  });
});
