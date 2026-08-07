import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { MonacoNS } from "@/lib/file-preview/monaco-loader";

import { MarkdownSourceView } from "../markdown-source-view";

function createFakeMonaco() {
  const editor = {
    create: vi.fn(
      (_container: HTMLElement, options: Record<string, unknown>) => ({
        options,
        setValue: vi.fn(),
        dispose: vi.fn(),
      }),
    ),
  };
  const monaco = { editor } as unknown as MonacoNS;
  return { monaco, editor };
}

describe("MarkdownSourceView", () => {
  it("renders raw markdown source through monaco with the markdown language", () => {
    const { monaco, editor } = createFakeMonaco();

    render(<MarkdownSourceView value={"# Title\n\nbody"} monaco={monaco} />);

    expect(editor.create).toHaveBeenCalledTimes(1);
    const [, options] = editor.create.mock.calls[0];
    expect(options).toMatchObject({
      readOnly: true,
      language: "markdown",
      value: "# Title\n\nbody",
    });
  });
});
