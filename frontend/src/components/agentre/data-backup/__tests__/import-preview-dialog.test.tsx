import "@testing-library/jest-dom/vitest";

import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";

import {
  ImportPreviewDialog,
  type PreviewItem,
} from "../import-preview-dialog";

function makeItem(over: Partial<PreviewItem> = {}): PreviewItem {
  return {
    scope: "llm-providers",
    sourceKey: "k1",
    name: "P1",
    defaultAction: "create",
    ...over,
  };
}

describe("ImportPreviewDialog", () => {
  it("dangling 行显示引用缺失 badge 及提示", () => {
    const apply = vi.fn();
    render(
      <ImportPreviewDialog
        open
        onOpenChange={() => {}}
        preview={{
          secretsIncluded: true,
          items: [
            makeItem({
              dangling: true,
              danglingHint: "所属后端不在范围内",
              defaultAction: "skip",
            }),
          ],
        }}
        onApply={apply}
      />,
    );
    expect(screen.getByText("Missing Reference")).toBeInTheDocument();
    expect(screen.getByText("所属后端不在范围内")).toBeInTheDocument();
  });

  it("应用时把 actions 传出去", () => {
    const apply = vi.fn().mockResolvedValue(undefined);
    render(
      <ImportPreviewDialog
        open
        onOpenChange={() => {}}
        preview={{ secretsIncluded: true, items: [makeItem()] }}
        onApply={apply}
      />,
    );
    fireEvent.click(screen.getByText("Apply"));
    expect(apply).toHaveBeenCalledWith(
      { "llm-providers:k1": "create" },
      "skip",
    );
  });
});
