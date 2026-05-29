import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const sonnerMocks = vi.hoisted(() => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

vi.mock("sonner", () => sonnerMocks);

import { MarkdownText } from "./markdown-text";

const originalClipboard = navigator.clipboard;

function mockClipboard() {
  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: { writeText },
  });
  return writeText;
}

afterEach(() => {
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: originalClipboard,
  });
});

describe("MarkdownText", () => {
  it("copies fenced code block text from AI markdown replies", async () => {
    const writeText = mockClipboard();

    render(<MarkdownText text={"结果如下：\n\n```\npnpm test\n```\n"} />);

    fireEvent.click(screen.getByRole("button", { name: "Copy" }));

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith("pnpm test\n");
    });
    expect(sonnerMocks.toast.success).toHaveBeenCalledWith(
      "Code copied",
      expect.objectContaining({
        duration: 5000,
        position: "bottom-right",
      }),
    );
    expect(screen.getByRole("button", { name: "Copied" })).toBeInTheDocument();
  });
});

describe("MarkdownText URL whitelist", () => {
  it("preserves https href as-is", () => {
    const { container } = render(
      <MarkdownText text="[ex](https://example.com)" />,
    );
    expect(container.querySelector("a")?.getAttribute("href")).toBe(
      "https://example.com",
    );
  });

  it("preserves absolute POSIX path href as-is", () => {
    const { container } = render(
      <MarkdownText text="[f](/Users/me/foo.go:42)" cwd="/Users/me" />,
    );
    expect(container.querySelector("a")?.getAttribute("href")).toBe(
      "/Users/me/foo.go:42",
    );
  });

  it("resolves file:// href to local path (RichLink handles it)", () => {
    const { container } = render(
      <MarkdownText text="[f](file:///Users/me/foo.go)" />,
    );
    const a = container.querySelector("a");
    // RichLink resolves file:// → local path via classifyLink/fullTarget
    expect(a?.getAttribute("href")).toBe("/Users/me/foo.go");
  });

  it("strips javascript: href", () => {
    const { container } = render(
      <MarkdownText text="[x](javascript:alert(1))" />,
    );
    const a = container.querySelector("a");
    // After url whitelist strips href, RichLink renders plain anchor with no href.
    expect(a?.getAttribute("href")).toBeFalsy();
  });
});
