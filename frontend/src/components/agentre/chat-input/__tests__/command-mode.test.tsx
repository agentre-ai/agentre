import { act, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createRef, type RefObject } from "react";
import { beforeEach, describe, expect, it, onTestFinished, vi } from "vitest";

import type { Editor } from "@tiptap/react";

import { useLocalCommandsStore } from "@/stores/local-commands-store";
import { localCommandHistoryStore } from "@/stores/local-command-history-store";

import { AIChatInput } from "../index";
import type { LocalCommandHistoryScope } from "../local-command-history/types";
import type { AIChatInputHandle } from "../types";

const repoScope: LocalCommandHistoryScope = {
  deviceId: "device-command-mode",
  cwd: "/repo/command-mode",
};
const otherScope: LocalCommandHistoryScope = {
  deviceId: "device-command-mode",
  cwd: "/repo/other",
};

beforeEach(() => {
  vi.restoreAllMocks();
  localCommandHistoryStore.clear(repoScope);
  localCommandHistoryStore.clear(otherScope);
  useLocalCommandsStore.setState({ entries: {} });
});

/** 在编辑器的 contentEditable DOM 上派发一次 keydown，驱动 TipTap 菜单/提交路径。 */
function pressKey(editor: Editor, key: string) {
  const event = new KeyboardEvent("keydown", {
    key,
    bubbles: true,
    cancelable: true,
  });
  editor.view.dom.dispatchEvent(event);
}

function pressEnter(editor: Editor) {
  pressKey(editor, "Enter");
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

  it("does not call onCommandSubmit or reserve history order for bare ! (empty command)", () => {
    const onCommandSubmit = vi.fn();
    const onSubmit = vi.fn();
    const reserveSpy = vi.spyOn(localCommandHistoryStore, "reserveLastUsedAt");
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
    expect(reserveSpy).not.toHaveBeenCalled();
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

  it("Given active-scope history, When a spaced ! query is typed, Then only ranked matches from that scope open in an accessible menu", async () => {
    localCommandHistoryStore.record(repoScope, "git status", 10);
    localCommandHistoryStore.record(repoScope, "git checkout main", 30);
    localCommandHistoryStore.record(otherScope, "git checkout secret", 40);
    const editorRef: RefObject<Editor | null> = { current: null };

    render(
      <AIChatInput
        editorRef={editorRef}
        localCommandHistoryScope={repoScope}
        onSubmit={vi.fn()}
        onCommandSubmit={vi.fn()}
      />,
    );

    act(() => {
      editorRef.current!.commands.insertContent("!git ch ma");
    });

    const menu = await screen.findByRole("listbox", {
      name: "Shell command history",
    });
    expect(menu).toBeInTheDocument();
    expect(
      screen.getByRole("option", { name: "git checkout main" }),
    ).toHaveAttribute("aria-selected", "true");
    expect(screen.queryByText("git checkout secret")).not.toBeInTheDocument();
    expect(screen.queryByText("git status")).not.toBeInTheDocument();
  });

  it("Given history exists, When input is not in ! mode or the full query has no match, Then no empty menu is rendered", async () => {
    localCommandHistoryStore.record(repoScope, "git status", 10);
    const editorRef: RefObject<Editor | null> = { current: null };

    render(
      <AIChatInput
        editorRef={editorRef}
        localCommandHistoryScope={repoScope}
        onSubmit={vi.fn()}
        onCommandSubmit={vi.fn()}
      />,
    );

    act(() => {
      editorRef.current!.commands.insertContent("git");
    });
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();

    await act(async () => {
      editorRef.current!.commands.setContent("!no matching command");
      editorRef.current!.commands.focus("end");
      await Promise.resolve();
    });
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
  });

  it("Given scoped history, When arrows move between command rows and footer Clear, Then only rows are options and editor ARIA follows its focused row", async () => {
    localCommandHistoryStore.record(repoScope, "git status", 30);
    localCommandHistoryStore.record(repoScope, "git stash", 20);
    const editorRef: RefObject<Editor | null> = { current: null };
    const user = userEvent.setup();

    render(
      <AIChatInput
        editorRef={editorRef}
        localCommandHistoryScope={repoScope}
        onSubmit={vi.fn()}
        onCommandSubmit={vi.fn()}
      />,
    );
    const editor = editorRef.current!;

    act(() => {
      editor.commands.insertContent("!git");
      editor.commands.focus("end");
    });

    const listbox = await screen.findByRole("listbox", {
      name: "Shell command history",
    });
    const firstOption = screen.getByRole("option", { name: "git status" });
    const secondOption = screen.getByRole("option", { name: "git stash" });
    const clearButton = screen.getByRole("button", {
      name: "Clear history for current directory",
    });
    const combobox = screen.getByRole("combobox");
    const listboxId = listbox.id;
    const firstOptionId = firstOption.id;
    const secondOptionId = secondOption.id;

    expect(combobox).toHaveFocus();
    expect(listboxId).not.toBe("");
    expect(firstOptionId).not.toBe("");
    expect(secondOptionId).not.toBe("");
    expect(firstOptionId).not.toBe(secondOptionId);
    expect(listbox).not.toContainElement(clearButton);
    expect(
      screen.queryByRole("option", {
        name: "Clear history for current directory",
      }),
    ).not.toBeInTheDocument();
    expect(clearButton).not.toHaveAttribute("aria-selected");
    expect(combobox).toHaveAttribute("aria-expanded", "true");
    expect(combobox).toHaveAttribute("aria-haspopup", "listbox");
    expect(combobox).toHaveAttribute("aria-controls", listboxId);
    expect(combobox).toHaveAttribute("aria-activedescendant", firstOptionId);
    expect(firstOption).toHaveAttribute("aria-selected", "true");

    act(() => pressKey(editor, "ArrowUp"));
    expect(clearButton).toHaveFocus();
    expect(combobox).not.toHaveAttribute("aria-activedescendant");
    expect(firstOption).toHaveAttribute("aria-selected", "false");
    expect(secondOption).toHaveAttribute("aria-selected", "false");

    await user.keyboard("{ArrowUp}");
    expect(combobox).toHaveFocus();
    expect(combobox).toHaveAttribute("aria-activedescendant", secondOptionId);
    expect(secondOption).toHaveAttribute("aria-selected", "true");

    act(() => pressKey(editor, "ArrowDown"));
    expect(clearButton).toHaveFocus();
    expect(combobox).not.toHaveAttribute("aria-activedescendant");

    await user.keyboard("{Escape}");
    const textbox = screen.getByRole("textbox");
    expect(textbox).toHaveFocus();
    expect(textbox).not.toHaveAttribute("aria-expanded");
    expect(textbox).not.toHaveAttribute("aria-haspopup");
    expect(textbox).not.toHaveAttribute("aria-controls");
    expect(textbox).not.toHaveAttribute("aria-activedescendant");
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
  });

  it("Given cursor coordinates are unavailable, When Enter submits a matching ! query, Then no hidden history row consumes the command", () => {
    localCommandHistoryStore.record(repoScope, "git status", 10);
    const editorRef: RefObject<Editor | null> = { current: null };
    const onCommandSubmit = vi.fn();

    render(
      <AIChatInput
        editorRef={editorRef}
        localCommandHistoryScope={repoScope}
        onSubmit={vi.fn()}
        onCommandSubmit={onCommandSubmit}
      />,
    );
    const editor = editorRef.current!;
    vi.spyOn(editor.view, "coordsAtPos").mockImplementation(() => {
      throw new Error("editor view is not measurable");
    });

    act(() => {
      editor.commands.insertContent("!git");
    });
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();

    act(() => {
      pressEnter(editor);
    });

    expect(onCommandSubmit).toHaveBeenCalledWith("git");
    expect(editor.getText()).toBe("");
  });

  it.each(["Enter", "Tab"])(
    "Given ranked history, When ArrowDown and %s choose a row, Then the full ! body is replaced without execution and the next submit records under the returned execution scope",
    async (selectionKey) => {
      localCommandHistoryStore.record(repoScope, "git cherry-pick master", 10);
      localCommandHistoryStore.record(repoScope, "git checkout main", 30);
      const editorRef: RefObject<Editor | null> = { current: null };
      const events: string[] = [];
      const originalRecord = localCommandHistoryStore.record.bind(
        localCommandHistoryStore,
      );
      const recordSpy = vi
        .spyOn(localCommandHistoryStore, "record")
        .mockImplementation((scope, command, lastUsedAt) => {
          events.push(`record:${command}`);
          originalRecord(scope, command, lastUsedAt);
        });
      const onCommandSubmit = vi.fn((command: string) => {
        events.push(`submit:${command}`);
        return repoScope;
      });
      const onSubmit = vi.fn();

      render(
        <AIChatInput
          editorRef={editorRef}
          localCommandHistoryScope={repoScope}
          onSubmit={onSubmit}
          onCommandSubmit={onCommandSubmit}
        />,
      );
      const editor = editorRef.current!;

      act(() => {
        editor.commands.insertContent("!git ch ma");
      });
      await screen.findByRole("option", { name: "git checkout main" });

      act(() => {
        pressKey(editor, "ArrowDown");
      });
      expect(
        screen.getByRole("option", { name: "git cherry-pick master" }),
      ).toHaveAttribute("aria-selected", "true");
      act(() => {
        pressKey(editor, "ArrowUp");
      });
      expect(
        screen.getByRole("option", { name: "git checkout main" }),
      ).toHaveAttribute("aria-selected", "true");
      act(() => {
        pressKey(editor, "ArrowDown");
      });
      expect(
        screen.getByRole("option", { name: "git cherry-pick master" }),
      ).toHaveAttribute("aria-selected", "true");

      act(() => {
        pressKey(editor, selectionKey);
      });

      expect(editor.getText()).toBe("!git cherry-pick master");
      act(() => {
        editor.commands.insertContent(" --no-verify");
      });
      expect(editor.getText()).toBe("!git cherry-pick master --no-verify");
      expect(onCommandSubmit).not.toHaveBeenCalled();
      expect(onSubmit).not.toHaveBeenCalled();
      expect(recordSpy).not.toHaveBeenCalled();
      expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
      expect(localCommandHistoryStore.list(repoScope)[1]).toEqual({
        command: "git cherry-pick master",
        lastUsedAt: 10,
      });

      act(() => {
        editor.commands.setTextSelection(1);
        editor.commands.setTextSelection(editor.state.doc.content.size - 1);
      });
      expect(screen.queryByRole("listbox")).not.toBeInTheDocument();

      act(() => {
        editor.commands.setContent("!   git cherry-pick master   ");
        editor.commands.focus("end");
      });
      await screen.findByRole("listbox", { name: "Shell command history" });
      act(() => {
        pressKey(editor, "Escape");
      });
      expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
      act(() => {
        pressEnter(editor);
      });

      await vi.waitFor(() => {
        expect(events).toEqual([
          "submit:git cherry-pick master",
          "record:git cherry-pick master",
        ]);
      });
      expect(onCommandSubmit).toHaveBeenCalledWith("git cherry-pick master");
      expect(editor.getText()).toBe("");
    },
  );

  it("Given two nonempty command submissions, When their execution scopes resolve in reverse order, Then each reserves singleton history order immediately before its handler and MRU follows submission order", async () => {
    let resolveFirst!: (scope: LocalCommandHistoryScope) => void;
    let resolveSecond!: (scope: LocalCommandHistoryScope) => void;
    const firstScope = new Promise<LocalCommandHistoryScope>((resolve) => {
      resolveFirst = resolve;
    });
    const secondScope = new Promise<LocalCommandHistoryScope>((resolve) => {
      resolveSecond = resolve;
    });
    const events: string[] = [];
    const reserveSpy = vi
      .spyOn(localCommandHistoryStore, "reserveLastUsedAt")
      .mockImplementationOnce(() => {
        events.push("reserve:100");
        return 100;
      })
      .mockImplementationOnce(() => {
        events.push("reserve:101");
        return 101;
      });
    const onCommandSubmit = vi
      .fn()
      .mockImplementationOnce((command: string) => {
        events.push(`submit:${command}`);
        return firstScope;
      })
      .mockImplementationOnce((command: string) => {
        events.push(`submit:${command}`);
        return secondScope;
      });
    const recordSpy = vi.spyOn(localCommandHistoryStore, "record");
    const editorRef: RefObject<Editor | null> = { current: null };

    render(
      <AIChatInput
        editorRef={editorRef}
        localCommandHistoryScope={repoScope}
        onSubmit={vi.fn()}
        onCommandSubmit={onCommandSubmit}
      />,
    );
    const editor = editorRef.current!;

    act(() => {
      editor.commands.insertContent("!first command");
      pressEnter(editor);
      editor.commands.insertContent("!second command");
      pressEnter(editor);
    });

    expect(events).toEqual([
      "reserve:100",
      "submit:first command",
      "reserve:101",
      "submit:second command",
    ]);
    expect(reserveSpy).toHaveBeenCalledTimes(2);

    await act(async () => {
      resolveSecond(repoScope);
      await secondScope;
      resolveFirst(repoScope);
      await firstScope;
    });

    expect(onCommandSubmit.mock.calls).toEqual([
      ["first command"],
      ["second command"],
    ]);
    expect(recordSpy.mock.calls).toEqual([
      [repoScope, "second command", 101],
      [repoScope, "first command", 100],
    ]);

    act(() => {
      editor.commands.insertContent("!");
    });
    const options = await screen.findAllByRole("option");
    expect(options.map((option) => option.textContent)).toEqual([
      "second command",
      "first command",
    ]);
    expect(
      screen.getByRole("button", {
        name: "Clear history for current directory",
      }),
    ).toBeInTheDocument();
  });

  it("Given a dismissed history menu, When the query is unchanged, Then it stays closed until the command body changes", async () => {
    localCommandHistoryStore.record(repoScope, "git status", 10);
    localCommandHistoryStore.record(repoScope, "git stash", 20);
    const editorRef: RefObject<Editor | null> = { current: null };

    render(
      <AIChatInput
        editorRef={editorRef}
        localCommandHistoryScope={repoScope}
        onSubmit={vi.fn()}
        onCommandSubmit={vi.fn()}
      />,
    );
    const editor = editorRef.current!;

    act(() => {
      editor.commands.insertContent("!git");
    });
    await screen.findByRole("listbox", { name: "Shell command history" });

    act(() => {
      pressKey(editor, "Escape");
    });
    expect(editor.getText()).toBe("!git");
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();

    act(() => {
      editor.commands.setTextSelection(1);
      editor.commands.setTextSelection(editor.state.doc.content.size - 1);
    });
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();

    act(() => {
      editor.commands.insertContent(" st");
    });
    expect(
      await screen.findByRole("option", { name: "git status" }),
    ).toBeInTheDocument();
  });

  it("Given scope-specific history, When the active device/cwd scope changes, Then the open menu switches immediately without mixing rows", async () => {
    localCommandHistoryStore.record(repoScope, "repo command", 10);
    localCommandHistoryStore.record(otherScope, "other command", 20);
    const editorRef: RefObject<Editor | null> = { current: null };

    const { rerender } = render(
      <AIChatInput
        editorRef={editorRef}
        localCommandHistoryScope={repoScope}
        onSubmit={vi.fn()}
        onCommandSubmit={vi.fn()}
      />,
    );

    act(() => {
      editorRef.current!.commands.insertContent("!");
    });
    expect(
      await screen.findByRole("option", { name: "repo command" }),
    ).toBeInTheDocument();

    rerender(
      <AIChatInput
        editorRef={editorRef}
        localCommandHistoryScope={otherScope}
        onSubmit={vi.fn()}
        onCommandSubmit={vi.fn()}
      />,
    );

    expect(
      await screen.findByRole("option", { name: "other command" }),
    ).toBeInTheDocument();
    expect(screen.queryByText("repo command")).not.toBeInTheDocument();
  });

  it("Given a rejected command handler promise, When a nonempty command is submitted, Then no history is recorded and the rejection is consumed", async () => {
    const editorRef: RefObject<Editor | null> = { current: null };
    const rejection = new Error("terminal rpc failed");
    const onCommandSubmit = vi.fn().mockRejectedValue(rejection);
    const recordSpy = vi.spyOn(localCommandHistoryStore, "record");
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});

    render(
      <AIChatInput
        editorRef={editorRef}
        localCommandHistoryScope={repoScope}
        onSubmit={vi.fn()}
        onCommandSubmit={onCommandSubmit}
      />,
    );

    act(() => {
      editorRef.current!.commands.insertContent("!pwd");
      pressEnter(editorRef.current!);
    });

    await vi.waitFor(() => {
      expect(warnSpy).toHaveBeenCalledWith(
        "[chat-input] local command submission failed",
        rejection,
      );
    });
    expect(recordSpy).not.toHaveBeenCalled();
    expect(editorRef.current!.getText()).toBe("");
  });

  it("Given history order reservation fails, When a nonempty command is submitted, Then execution still runs exactly once and the history failure is consumed", async () => {
    const editorRef: RefObject<Editor | null> = { current: null };
    const reservationFailure = new RangeError("timestamp budget exhausted");
    const onCommandSubmit = vi.fn().mockResolvedValue(repoScope);
    vi.spyOn(localCommandHistoryStore, "reserveLastUsedAt").mockImplementation(
      () => {
        throw reservationFailure;
      },
    );
    const recordSpy = vi.spyOn(localCommandHistoryStore, "record");
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
    const unhandledRejection = vi.fn();
    window.addEventListener("unhandledrejection", unhandledRejection);
    onTestFinished(() =>
      window.removeEventListener("unhandledrejection", unhandledRejection),
    );

    render(
      <AIChatInput
        editorRef={editorRef}
        localCommandHistoryScope={repoScope}
        onSubmit={vi.fn()}
        onCommandSubmit={onCommandSubmit}
      />,
    );

    expect(() => {
      act(() => {
        editorRef.current!.commands.insertContent("!pwd");
        pressEnter(editorRef.current!);
      });
    }).not.toThrow();
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(onCommandSubmit).toHaveBeenCalledTimes(1);
    expect(onCommandSubmit).toHaveBeenCalledWith("pwd");
    expect(recordSpy).not.toHaveBeenCalled();
    expect(warnSpy).toHaveBeenCalledWith(
      "[chat-input] failed to reserve local command history order",
      reservationFailure,
    );
    expect(unhandledRejection).not.toHaveBeenCalled();
    expect(editorRef.current!.getText()).toBe("");
  });

  it("Given history recording rejects, When command execution succeeds, Then the optional write failure is consumed without repeating execution", async () => {
    const editorRef: RefObject<Editor | null> = { current: null };
    const recordFailure = new Error("history persistence failed");
    const onCommandSubmit = vi.fn().mockResolvedValue(repoScope);
    const recordSpy = vi
      .spyOn(localCommandHistoryStore, "record")
      .mockImplementation(() => Promise.reject(recordFailure) as never);
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
    const unhandledRejection = vi.fn();
    window.addEventListener("unhandledrejection", unhandledRejection);
    onTestFinished(() =>
      window.removeEventListener("unhandledrejection", unhandledRejection),
    );

    render(
      <AIChatInput
        editorRef={editorRef}
        localCommandHistoryScope={repoScope}
        onSubmit={vi.fn()}
        onCommandSubmit={onCommandSubmit}
      />,
    );

    act(() => {
      editorRef.current!.commands.insertContent("!pwd");
      pressEnter(editorRef.current!);
    });

    await vi.waitFor(() => {
      expect(warnSpy).toHaveBeenCalledWith(
        "[chat-input] failed to record local command history",
        recordFailure,
      );
    });
    expect(onCommandSubmit).toHaveBeenCalledTimes(1);
    expect(recordSpy).toHaveBeenCalledTimes(1);
    expect(unhandledRejection).not.toHaveBeenCalled();
  });

  it("Given history reads fail, When a command is entered and submitted, Then the menu stays unavailable without blocking execution", () => {
    const editorRef: RefObject<Editor | null> = { current: null };
    const onCommandSubmit = vi.fn();
    vi.spyOn(localCommandHistoryStore, "list").mockImplementation(() => {
      throw new Error("history read failed");
    });
    vi.spyOn(console, "warn").mockImplementation(() => {});

    render(
      <AIChatInput
        editorRef={editorRef}
        localCommandHistoryScope={repoScope}
        onSubmit={vi.fn()}
        onCommandSubmit={onCommandSubmit}
      />,
    );

    act(() => {
      editorRef.current!.commands.insertContent("!pwd");
    });
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();

    act(() => {
      pressEnter(editorRef.current!);
    });
    expect(onCommandSubmit).toHaveBeenCalledWith("pwd");
  });

  it.each([
    { activationKey: "Enter", userInput: "{Enter}" },
    { activationKey: "Space", userInput: " " },
  ])(
    "Given scoped history and an editor draft, When arrow wrap focuses Clear and native $activationKey activates it, Then only that history is cleared without submitting",
    async ({ userInput }) => {
      localCommandHistoryStore.record(repoScope, "git status", 30);
      localCommandHistoryStore.record(repoScope, "git stash", 20);
      localCommandHistoryStore.record(otherScope, "other command", 10);
      const editorRef: RefObject<Editor | null> = { current: null };
      const onCommandSubmit = vi.fn();
      const onSubmit = vi.fn();
      const user = userEvent.setup();

      render(
        <AIChatInput
          editorRef={editorRef}
          localCommandHistoryScope={repoScope}
          onSubmit={onSubmit}
          onCommandSubmit={onCommandSubmit}
        />,
      );
      const editor = editorRef.current!;

      act(() => {
        editor.commands.insertContent("!git");
        editor.commands.focus("end");
      });
      await screen.findByRole("option", { name: "git status" });
      const clearButton = screen.getByRole("button", {
        name: "Clear history for current directory",
      });

      act(() => pressKey(editor, "ArrowUp"));
      expect(clearButton).toHaveFocus();
      expect(screen.getByRole("combobox")).not.toHaveAttribute(
        "aria-activedescendant",
      );

      await user.keyboard(userInput);

      expect(editor.getText()).toBe("!git");
      expect(screen.getByRole("textbox")).toHaveFocus();
      expect(onCommandSubmit).not.toHaveBeenCalled();
      expect(onSubmit).not.toHaveBeenCalled();
      expect(localCommandHistoryStore.list(repoScope)).toEqual([]);
      expect(localCommandHistoryStore.list(otherScope)).toEqual([
        { command: "other command", lastUsedAt: 10 },
      ]);
      expect(screen.queryByRole("listbox")).not.toBeInTheDocument();

      act(() => {
        editor.commands.insertContent("x");
      });
      expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
    },
  );

  it("Given footer Clear is focused, When Shift+Tab or Tab is pressed, Then native focus moves without clearing, filling, or submitting the draft", async () => {
    localCommandHistoryStore.record(repoScope, "git status", 30);
    const editorRef: RefObject<Editor | null> = { current: null };
    const onCommandSubmit = vi.fn();
    const onSubmit = vi.fn();
    const user = userEvent.setup();

    render(
      <>
        <AIChatInput
          editorRef={editorRef}
          localCommandHistoryScope={repoScope}
          onSubmit={onSubmit}
          onCommandSubmit={onCommandSubmit}
        />
        <button type="button">After composer</button>
      </>,
    );
    const editor = editorRef.current!;

    act(() => {
      editor.commands.insertContent("!git");
      editor.commands.focus("end");
    });
    await screen.findByRole("listbox", { name: "Shell command history" });

    const firstOption = screen.getByRole("option", { name: "git status" });
    const clearButton = screen.getByRole("button", {
      name: "Clear history for current directory",
    });
    const combobox = screen.getByRole("combobox");

    act(() => pressKey(editor, "ArrowUp"));
    expect(clearButton).toHaveFocus();

    await user.tab({ shift: true });

    expect(combobox).toHaveFocus();
    expect(combobox).toHaveAttribute("aria-activedescendant", firstOption.id);
    expect(firstOption).toHaveAttribute("aria-selected", "true");
    expect(localCommandHistoryStore.list(repoScope)).toHaveLength(1);

    act(() => pressKey(editor, "ArrowUp"));
    expect(clearButton).toHaveFocus();
    await user.tab();

    expect(
      screen.getByRole("button", { name: "After composer" }),
    ).toHaveFocus();
    expect(editor.getText()).toBe("!git");
    expect(onCommandSubmit).not.toHaveBeenCalled();
    expect(onSubmit).not.toHaveBeenCalled();
    expect(localCommandHistoryStore.list(repoScope)).toEqual([
      { command: "git status", lastUsedAt: 30 },
    ]);
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
  });

  it("Given a long command and transient output, When history is hovered, picked, or cleared, Then dynamic text stays complete and clearing preserves the draft and output card", async () => {
    const longCommand = `printf '${"x".repeat(180)}'`;
    localCommandHistoryStore.record(repoScope, longCommand, 30);
    localCommandHistoryStore.record(repoScope, "git status", 20);
    localCommandHistoryStore.record(otherScope, "other command", 10);
    useLocalCommandsStore.getState().start({
      id: "running-command",
      sessionId: 42,
      command: "sleep 10",
      createdAt: 1,
    });
    const editorRef: RefObject<Editor | null> = { current: null };
    const onCommandSubmit = vi.fn();

    render(
      <AIChatInput
        editorRef={editorRef}
        localCommandHistoryScope={repoScope}
        onSubmit={vi.fn()}
        onCommandSubmit={onCommandSubmit}
      />,
    );
    const editor = editorRef.current!;

    act(() => {
      editor.commands.insertContent("!");
    });
    const longOption = await screen.findByRole("option", {
      name: longCommand,
    });
    expect(longOption.querySelector("span")).toHaveClass("truncate");

    const statusOption = screen.getByRole("option", { name: "git status" });
    fireEvent.mouseMove(statusOption);
    expect(statusOption).toHaveAttribute("aria-selected", "true");
    fireEvent.mouseDown(statusOption);
    expect(editor.getText()).toBe("!git status");
    expect(onCommandSubmit).not.toHaveBeenCalled();

    act(() => {
      editor.commands.insertContent(" ");
    });
    const historyMenu = await screen.findByRole("listbox", {
      name: "Shell command history",
    });
    const clearButton = screen.getByRole("button", {
      name: "Clear history for current directory",
    });
    expect(historyMenu).not.toContainElement(clearButton);
    fireEvent.click(clearButton);

    expect(editor.getText()).toBe("!git status ");
    expect(localCommandHistoryStore.list(repoScope)).toEqual([]);
    expect(localCommandHistoryStore.list(otherScope)).toEqual([
      { command: "other command", lastUsedAt: 10 },
    ]);
    expect(
      useLocalCommandsStore.getState().get("running-command"),
    ).toMatchObject({
      command: "sleep 10",
      output: "",
      status: "running",
    });
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
  });
});
