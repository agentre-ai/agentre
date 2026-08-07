import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { MonacoNS } from "@/lib/file-preview/monaco-loader";

import { DiffPreview } from "../diff-view";

type FakeModel = {
  value: string;
  language: string;
  dispose: ReturnType<typeof vi.fn>;
};

type FakeDiffEditor = {
  options: Record<string, unknown>;
  setModel: ReturnType<typeof vi.fn>;
  getModel: ReturnType<typeof vi.fn>;
  dispose: ReturnType<typeof vi.fn>;
};

function createFakeMonaco() {
  const models: FakeModel[] = [];
  const diffs: FakeDiffEditor[] = [];
  const editor = {
    setTheme: vi.fn(),
    createDiffEditor: vi.fn(
      (
        _container: HTMLElement,
        options: Record<string, unknown>,
      ): FakeDiffEditor => {
        const d: FakeDiffEditor = {
          options,
          setModel: vi.fn(),
          getModel: vi.fn(() => null),
          dispose: vi.fn(),
        };
        diffs.push(d);
        return d;
      },
    ),
    createModel: vi.fn((value: string, language: string): FakeModel => {
      const m: FakeModel = { value, language, dispose: vi.fn() };
      models.push(m);
      return m;
    }),
  };
  const monaco = { editor } as unknown as MonacoNS;
  return { monaco, editor, models, diffs };
}

describe("DiffPreview", () => {
  it("creates a readOnly diff editor with original/modified models", () => {
    const { monaco, editor, models, diffs } = createFakeMonaco();

    render(
      <DiffPreview
        original={"line1\nline2"}
        modified={"line1\nline3"}
        path="a.ts"
        monaco={monaco}
      />,
    );

    expect(editor.createDiffEditor).toHaveBeenCalledTimes(1);
    const [container, options] = editor.createDiffEditor.mock.calls[0];
    expect(container).toBeInstanceOf(HTMLElement);
    expect(options).toMatchObject({
      readOnly: true,
      renderSideBySide: true,
    });

    expect(editor.createModel).toHaveBeenCalledTimes(2);
    expect(models[0]).toMatchObject({
      value: "line1\nline2",
      language: "typescript",
    });
    expect(models[1]).toMatchObject({
      value: "line1\nline3",
      language: "typescript",
    });

    expect(diffs[0].setModel).toHaveBeenCalledWith({
      original: models[0],
      modified: models[1],
    });
  });

  it("rebuilds the diff when content changes and disposes the previous editor", () => {
    const { monaco, editor, diffs } = createFakeMonaco();

    const { rerender } = render(
      <DiffPreview original="a" modified="b" path="x.txt" monaco={monaco} />,
    );
    rerender(
      <DiffPreview original="a2" modified="b2" path="x.txt" monaco={monaco} />,
    );

    expect(editor.createDiffEditor).toHaveBeenCalledTimes(2);
    expect(diffs[0].dispose).toHaveBeenCalledTimes(1);
  });

  it("disposes the diff editor on unmount", () => {
    const { monaco, diffs } = createFakeMonaco();

    const { unmount } = render(
      <DiffPreview original="a" modified="b" path="x.go" monaco={monaco} />,
    );
    unmount();

    expect(diffs[0].dispose).toHaveBeenCalledTimes(1);
  });
});
