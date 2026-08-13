import { act, render, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { MonacoNS } from "@/lib/file-preview/monaco-loader";

import { CodePreview } from "../code-view";

// 真实 Monaco 在 happy-dom 里不可运行（worker / canvas）。测试走两条 mock 接缝：
// 1) monaco prop 注入 fake 命名空间（本文件的默认路径）；
// 2) vi.mock monaco-loader 返回 fake loadMonaco（下面 loadMonaco seam 测试）。
// fake monaco 只实现组件用到的 editor API 面，断言组件把正确的选项转发给 Monaco。
// vi.mock 必须位于模块顶层（vitest 未来会禁止嵌套）。
const loaderMocks = vi.hoisted(() => ({ loadMonaco: vi.fn() }));
vi.mock("@/lib/file-preview/monaco-loader", () => loaderMocks);

type FakeEditor = {
  options: Record<string, unknown>;
  setValue: ReturnType<typeof vi.fn>;
  dispose: ReturnType<typeof vi.fn>;
};

function createFakeMonaco() {
  const editors: FakeEditor[] = [];
  const editor = {
    setTheme: vi.fn(),
    create: vi.fn(
      (
        _container: HTMLElement,
        options: Record<string, unknown>,
      ): FakeEditor => {
        const e: FakeEditor = {
          options,
          setValue: vi.fn(),
          dispose: vi.fn(),
        };
        editors.push(e);
        return e;
      },
    ),
  };
  const monaco = { editor } as unknown as MonacoNS;
  return { monaco, editor, editors };
}

describe("CodePreview", () => {
  it("creates a readOnly monaco editor with the language inferred from path", () => {
    const { monaco, editor } = createFakeMonaco();

    render(<CodePreview value="const a = 1;" path="a.ts" monaco={monaco} />);

    expect(editor.create).toHaveBeenCalledTimes(1);
    const [container, options] = editor.create.mock.calls[0];
    expect(container).toBeInstanceOf(HTMLElement);
    expect(options).toMatchObject({
      readOnly: true,
      language: "typescript",
      value: "const a = 1;",
    });
  });

  it("defaults to plaintext for unknown extensions and honors an explicit language", () => {
    const { monaco, editor } = createFakeMonaco();

    const { rerender } = render(
      <CodePreview value="x" path="a.weird" monaco={monaco} />,
    );
    expect(editor.create.mock.calls[0][1].language).toBe("plaintext");

    rerender(
      <CodePreview value="x" path="a.ts" language="json" monaco={monaco} />,
    );
    expect(editor.create).toHaveBeenCalledTimes(2);
    expect(editor.create.mock.calls[1][1].language).toBe("json");
  });

  it("updates value on the same editor instance instead of recreating it", () => {
    const { monaco, editor, editors } = createFakeMonaco();

    const { rerender } = render(
      <CodePreview value="v1" path="notes.txt" monaco={monaco} />,
    );
    rerender(<CodePreview value="v2" path="notes.txt" monaco={monaco} />);

    expect(editor.create).toHaveBeenCalledTimes(1);
    expect(editors[0].setValue).toHaveBeenCalledWith("v2");
  });

  it("disposes the editor on unmount", () => {
    const { monaco, editors } = createFakeMonaco();

    const { unmount } = render(
      <CodePreview value="x" path="a.go" monaco={monaco} />,
    );
    unmount();

    expect(editors[0].dispose).toHaveBeenCalledTimes(1);
  });
});

describe("CodePreview loadMonaco seam", () => {
  // 不传 monaco prop 时组件应经 monaco-loader 动态加载 —— 这是 task 6 测试
  // vi.mock("@/lib/file-preview/monaco-loader") 时的接缝路径。

  it("loads monaco through the loader when no monaco prop is injected", async () => {
    const { monaco, editor } = createFakeMonaco();
    loaderMocks.loadMonaco.mockResolvedValue(monaco);

    render(<CodePreview value="x" path="a.go" />);

    expect(loaderMocks.loadMonaco).toHaveBeenCalled();
    await waitFor(() => expect(editor.create).toHaveBeenCalledTimes(1));
  });
});

describe("CodePreview theme following", () => {
  it("sets the monaco theme on mount and re-themes when the app flips .dark", () => {
    document.documentElement.classList.remove("dark");
    const { monaco, editor } = createFakeMonaco();
    const observe = vi.fn<MutationObserver["observe"]>();
    const disconnect = vi.fn<MutationObserver["disconnect"]>();

    class ControlledMutationObserver implements MutationObserver {
      constructor(readonly callback: MutationCallback) {
        mutationObservers.push(this);
      }

      observe = observe;
      disconnect = disconnect;
      takeRecords = vi.fn<MutationObserver["takeRecords"]>(() => []);
    }

    const mutationObservers: ControlledMutationObserver[] = [];

    vi.stubGlobal("MutationObserver", ControlledMutationObserver);
    try {
      const { unmount } = render(
        <CodePreview value="x" path="a.go" monaco={monaco} />,
      );

      expect(editor.create).toHaveBeenCalledTimes(1);
      expect(editor.setTheme).toHaveBeenLastCalledWith("vs");
      expect(observe).toHaveBeenCalledWith(document.documentElement, {
        attributes: true,
        attributeFilter: ["class"],
      });

      act(() => {
        document.documentElement.classList.add("dark");
        mutationObservers[0].callback([], mutationObservers[0]);
      });
      expect(editor.setTheme).toHaveBeenLastCalledWith("vs-dark");

      act(() => {
        document.documentElement.classList.remove("dark");
        mutationObservers[0].callback([], mutationObservers[0]);
      });
      expect(editor.setTheme).toHaveBeenLastCalledWith("vs");

      unmount();
      expect(disconnect).toHaveBeenCalledTimes(1);
    } finally {
      document.documentElement.classList.remove("dark");
      vi.unstubAllGlobals();
    }
  });
});
