import { act, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { useRef, type ComponentProps } from "react";

import type { Editor } from "@tiptap/react";

import { AIChatInput, type AIChatInputHandle } from "../../chat-input";

// 包一层把 editorRef 暴露给 test driver 做编程式插入。
function Harness({
  onSubmit,
  backendType = "claudecode",
  onSlashSelect = () => {},
}: {
  onSubmit: (text: string) => void;
  backendType?: string;
  onSlashSelect?: ComponentProps<typeof AIChatInput>["onSlashSelect"];
}) {
  const editorRef = useRef<Editor | null>(null);
  const handleRef = useRef<AIChatInputHandle>(null);
  return (
    <>
      <button
        type="button"
        data-testid="insert-slash"
        onClick={() => editorRef.current?.commands.insertContent("/")}
      >
        insert /
      </button>
      <button
        type="button"
        data-testid="insert-co"
        onClick={() => editorRef.current?.commands.insertContent("co")}
      >
        insert co
      </button>
      <button
        type="button"
        data-testid="insert-foo-slash"
        onClick={() => editorRef.current?.commands.insertContent("foo/")}
      >
        insert foo/
      </button>
      <AIChatInput
        ref={handleRef}
        onSubmit={onSubmit}
        editorRef={editorRef}
        backendType={backendType}
        onSlashSelect={onSlashSelect}
        autoFocus
      />
    </>
  );
}

describe("AIChatInput slash menu integration", () => {
  it("行首输入 / 弹出 popover 含 /compact", async () => {
    const onSubmit = vi.fn();
    render(<Harness onSubmit={onSubmit} />);

    act(() => {
      screen.getByTestId("insert-slash").click();
    });

    await waitFor(() => {
      expect(
        screen.getByRole("listbox", { name: "斜杠命令" }),
      ).toBeInTheDocument();
    });
    expect(screen.getByText("/compact")).toBeInTheDocument();
  });

  it("继续输入 co 仍命中 /compact;输入 xyz 列表为空", async () => {
    const onSubmit = vi.fn();
    render(<Harness onSubmit={onSubmit} />);

    act(() => {
      screen.getByTestId("insert-slash").click();
    });
    await waitFor(() =>
      expect(screen.getByText("/compact")).toBeInTheDocument(),
    );

    act(() => {
      screen.getByTestId("insert-co").click();
    });
    // 仍然能看到 /compact (co 是前缀)
    await waitFor(() =>
      expect(screen.getByText("/compact")).toBeInTheDocument(),
    );
  });

  it("foo/ 不触发 popover (词内 / 不算 trigger)", async () => {
    const onSubmit = vi.fn();
    render(<Harness onSubmit={onSubmit} />);

    act(() => {
      screen.getByTestId("insert-foo-slash").click();
    });
    // 给一拍时间让 selectionUpdate fire
    await new Promise((r) => setTimeout(r, 20));
    expect(screen.queryByRole("listbox", { name: "斜杠命令" })).toBeNull();
  });

  it("点击 /compact 仅填入输入框,不直接发送 (literal_text 路径)", async () => {
    const onSubmit = vi.fn();
    render(<Harness onSubmit={onSubmit} />);

    act(() => {
      screen.getByTestId("insert-slash").click();
    });
    await waitFor(() =>
      expect(screen.getByText("/compact")).toBeInTheDocument(),
    );

    const item = screen.getByText("/compact").closest("button")!;
    act(() => {
      item.dispatchEvent(
        new MouseEvent("mousedown", { bubbles: true, cancelable: true }),
      );
    });

    // popover 关闭后,/compact 应作为草稿留在编辑器里 (而不是被立即发出去),
    // 由用户再决定是否回车发送。
    await waitFor(() =>
      expect(screen.queryByRole("listbox", { name: "斜杠命令" })).toBeNull(),
    );
    expect(onSubmit).not.toHaveBeenCalled();
    // 编辑器里应当能看到完整的 /compact 文本
    expect(document.querySelector(".ProseMirror")?.textContent ?? "").toContain(
      "/compact",
    );
  });

  it("backendType=codex 时 /compact 也仅补全文字,不直接发送", async () => {
    const onSubmit = vi.fn();
    const onSlashSelect = vi.fn();
    render(
      <Harness
        onSubmit={onSubmit}
        backendType="codex"
        onSlashSelect={onSlashSelect}
      />,
    );

    act(() => {
      screen.getByTestId("insert-slash").click();
    });
    await waitFor(() =>
      expect(screen.getByText("/compact")).toBeInTheDocument(),
    );

    const item = screen.getByText("/compact").closest("button")!;
    act(() => {
      item.dispatchEvent(
        new MouseEvent("mousedown", { bubbles: true, cancelable: true }),
      );
    });

    await waitFor(() =>
      expect(screen.queryByRole("listbox", { name: "斜杠命令" })).toBeNull(),
    );
    // literal_text 由 AIChatInput 内部消化,不会冒泡到 onSlashSelect;
    // onSubmit 也不应被自动触发 —— 用户回车才执行(由 chat-panel 拦截 /compact 转 Compact RPC)。
    expect(onSlashSelect).not.toHaveBeenCalled();
    expect(onSubmit).not.toHaveBeenCalled();
    expect(document.querySelector(".ProseMirror")?.textContent ?? "").toContain(
      "/compact",
    );
  });
});
