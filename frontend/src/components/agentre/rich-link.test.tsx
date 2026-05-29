// frontend/src/components/agentre/rich-link.test.tsx
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const sonnerMocks = vi.hoisted(() => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));
vi.mock("sonner", () => sonnerMocks);

const openPathMock = vi.fn();
vi.mock("@/../wailsjs/go/app/App", () => ({
  OpenPath: (p: string) => openPathMock(p),
}));

const browserOpenURLMock = vi.fn();
vi.mock("@/../wailsjs/runtime/runtime", () => ({
  BrowserOpenURL: (u: string) => browserOpenURLMock(u),
}));

import { RichLink } from "./rich-link";

const CWD = "/Users/me/proj";

beforeEach(() => {
  openPathMock.mockReset();
  browserOpenURLMock.mockReset();
  sonnerMocks.toast.success.mockReset();
  sonnerMocks.toast.error.mockReset();
});

afterEach(() => {
  vi.useRealTimers();
});

function mockClipboard() {
  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: { writeText },
  });
  return writeText;
}

describe("RichLink", () => {
  describe("URL link", () => {
    it("clicking calls BrowserOpenURL, not browser navigation", () => {
      render(
        <RichLink href="https://example.com" cwd={CWD}>
          example
        </RichLink>,
      );
      const link = screen.getByRole("link", { name: /example/ });
      fireEvent.click(link);
      expect(browserOpenURLMock).toHaveBeenCalledWith("https://example.com");
      expect(openPathMock).not.toHaveBeenCalled();
    });

    it("renders open-link icon after text", () => {
      render(
        <RichLink href="https://example.com" cwd={CWD}>
          example
        </RichLink>,
      );
      expect(screen.getByTestId("rich-link-open-icon")).toHaveAttribute(
        "data-link-kind",
        "url",
      );
    });
  });

  describe("Local file link — in cwd", () => {
    it("clicking calls OpenPath with full path + line suffix", () => {
      render(
        <RichLink href="/Users/me/proj/src/foo.go:42" cwd={CWD}>
          foo.go:42
        </RichLink>,
      );
      const link = screen.getByRole("link", { name: /foo\.go:42/ });
      fireEvent.click(link);
      expect(openPathMock).toHaveBeenCalledWith("/Users/me/proj/src/foo.go:42");
      expect(browserOpenURLMock).not.toHaveBeenCalled();
    });

    it("renders file icon before text and open icon after text", () => {
      render(
        <RichLink href="/Users/me/proj/src/foo.go" cwd={CWD}>
          foo.go
        </RichLink>,
      );
      const link = screen.getByRole("link", { name: /foo\.go/ });
      expect(link.firstElementChild).toHaveAttribute(
        "data-testid",
        "rich-link-path-icon",
      );
      expect(screen.getByTestId("rich-link-path-icon")).toHaveAttribute(
        "data-path-kind",
        "file",
      );
      expect(screen.getByTestId("rich-link-open-icon")).toHaveAttribute(
        "data-link-kind",
        "local-internal",
      );
    });
  });

  describe("Local file link — outside cwd", () => {
    it("renders file icon, not folder icon", () => {
      render(
        <RichLink href="/usr/local/bin/agentred" cwd={CWD}>
          agentred
        </RichLink>,
      );
      expect(screen.getByTestId("rich-link-path-icon")).toHaveAttribute(
        "data-path-kind",
        "file",
      );
    });

    it("clicking calls OpenPath", () => {
      render(
        <RichLink href="/usr/local/bin/agentred" cwd={CWD}>
          agentred
        </RichLink>,
      );
      fireEvent.click(screen.getByRole("link", { name: /agentred/ }));
      expect(openPathMock).toHaveBeenCalledWith("/usr/local/bin/agentred");
    });
  });

  describe("Local folder link", () => {
    it("renders folder icon before text", () => {
      render(
        <RichLink href="/Users/me/proj/docs/" cwd={CWD}>
          docs
        </RichLink>,
      );
      expect(screen.getByTestId("rich-link-path-icon")).toHaveAttribute(
        "data-path-kind",
        "folder",
      );
    });
  });

  describe("Unknown / fallback", () => {
    it("renders plain anchor without icon for relative paths", () => {
      render(
        <RichLink href="relative/foo.go" cwd={CWD}>
          rel
        </RichLink>,
      );
      const link = screen.getByRole("link", { name: /rel/ });
      expect(link).toBeInTheDocument();
      expect(
        screen.queryByTestId("rich-link-path-icon"),
      ).not.toBeInTheDocument();
      expect(
        screen.queryByTestId("rich-link-open-icon"),
      ).not.toBeInTheDocument();
    });

    it("relative path click goes through default navigation (no mock called)", () => {
      render(
        <RichLink href="relative/foo.go" cwd={CWD}>
          rel
        </RichLink>,
      );
      const link = screen.getByRole("link", { name: /rel/ });
      link.addEventListener("click", (event) => event.preventDefault());

      fireEvent.click(link);
      expect(browserOpenURLMock).not.toHaveBeenCalled();
      expect(openPathMock).not.toHaveBeenCalled();
    });
  });

  describe("Copy button in popover", () => {
    it("URL popover copy writes full URL + shows success toast", async () => {
      const writeText = mockClipboard();
      render(
        <RichLink href="https://example.com/long/path" cwd={CWD}>
          ex
        </RichLink>,
      );
      const link = screen.getByRole("link", { name: /ex/ });
      fireEvent.focus(link);
      const copyBtn = await screen.findByRole("button", { name: /Copy/ });
      fireEvent.click(copyBtn);
      await waitFor(() => {
        expect(writeText).toHaveBeenCalledWith("https://example.com/long/path");
      });
      expect(sonnerMocks.toast.success).toHaveBeenCalled();
    });

    it("local-internal popover copy writes full path with line suffix", async () => {
      const writeText = mockClipboard();
      render(
        <RichLink href="/Users/me/proj/src/foo.go:42" cwd={CWD}>
          foo.go:42
        </RichLink>,
      );
      fireEvent.focus(screen.getByRole("link", { name: /foo\.go:42/ }));
      const copyBtn = await screen.findByRole("button", { name: /Copy/ });
      fireEvent.click(copyBtn);
      await waitFor(() => {
        expect(writeText).toHaveBeenCalledWith("/Users/me/proj/src/foo.go:42");
      });
    });
  });

  describe("Popover content sanity", () => {
    it("local-internal popover shows both project root and relative path", async () => {
      render(
        <RichLink href="/Users/me/proj/src/foo.go" cwd={CWD}>
          foo
        </RichLink>,
      );
      fireEvent.focus(screen.getByRole("link", { name: /foo/ }));
      expect(await screen.findByText("/Users/me/proj")).toBeInTheDocument();
      expect(screen.getByText("src/foo.go")).toBeInTheDocument();
    });

    it("local-internal popover wraps long project root and relative path segments", async () => {
      const cwd =
        "/Users/codfrm/Code/agentre/agentre/a-very-long-project-root-name";
      render(
        <RichLink
          href={`${cwd}/frontend/src/components/agentre/__tests__/chat.test.tsx:89`}
          cwd={cwd}
        >
          chat.test.tsx:89
        </RichLink>,
      );
      fireEvent.focus(screen.getByRole("link", { name: /chat\.test\.tsx:89/ }));

      expect(await screen.findByText(cwd)).toHaveClass(
        "min-w-0",
        "break-all",
        "whitespace-normal",
      );
      expect(
        screen.getByText(
          "frontend/src/components/agentre/__tests__/chat.test.tsx",
        ),
      ).toHaveClass("min-w-0", "break-all", "whitespace-normal");
    });

    it("local-external popover shows full path but no project root segment", async () => {
      render(
        <RichLink href="/usr/local/bin/agentred" cwd={CWD}>
          ag
        </RichLink>,
      );
      fireEvent.focus(screen.getByRole("link", { name: /ag/ }));
      expect(
        await screen.findByText("/usr/local/bin/agentred"),
      ).toBeInTheDocument();
      // CWD value should NOT appear in external popover.
      expect(screen.queryByText(CWD)).not.toBeInTheDocument();
    });
  });
});
