import "@testing-library/jest-dom/vitest";

import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const appMocks = vi.hoisted(() => ({
  ExportData: vi.fn().mockResolvedValue({
    path: "/tmp/x.json",
    canceled: false,
    summary: { "llm-providers": 1 },
  }),
}));

vi.mock("../../../../../wailsjs/go/app/App", () => appMocks);

const sonnerMocks = vi.hoisted(() => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("sonner", () => sonnerMocks);

import { ExportSection } from "../export-section";

describe("ExportSection", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("默认全选 4 个 scope", () => {
    render(<ExportSection />);
    const boxes = screen.getAllByRole("checkbox");
    expect(boxes).toHaveLength(4);
    boxes.forEach((b) => expect(b).toBeChecked());
  });

  it("勾选包含凭证后显示警告", () => {
    render(<ExportSection />);
    const sw = screen.getByRole("switch");
    fireEvent.click(sw);
    expect(screen.getByText(/plain-text secrets/)).toBeInTheDocument();
  });
});
