import { fireEvent, render, screen } from "@testing-library/react";
import * as React from "react";
import { describe, expect, it } from "vitest";

import { WorkflowEditorForm } from "./workflow-editor-form";

function Harness({ initialContent = "" }: { initialContent?: string }) {
  const [name, setName] = React.useState("");
  const [content, setContent] = React.useState(initialContent);
  return (
    <WorkflowEditorForm
      name={name}
      content={content}
      error={null}
      onNameChange={setName}
      onContentChange={setContent}
    />
  );
}

describe("WorkflowEditorForm", () => {
  it("编辑名称回写", () => {
    render(<Harness />);
    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), {
      target: { value: "评审流程" },
    });
    expect(
      (screen.getByRole("textbox", { name: "Name" }) as HTMLInputElement).value,
    ).toBe("评审流程");
  });

  it("空正文点插入模板:写入模板(以 # 开头)", () => {
    render(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: "Insert template" }));
    const ta = screen.getByRole("textbox", {
      name: "Workflow content (Markdown)",
    }) as HTMLTextAreaElement;
    expect(ta.value.startsWith("#")).toBe(true);
  });

  it("非空正文插入模板:追加不覆盖", () => {
    render(<Harness initialContent="已有内容" />);
    fireEvent.click(screen.getByRole("button", { name: "Insert template" }));
    const ta = screen.getByRole("textbox", {
      name: "Workflow content (Markdown)",
    }) as HTMLTextAreaElement;
    expect(ta.value.startsWith("已有内容")).toBe(true);
    expect(ta.value.length).toBeGreaterThan("已有内容".length);
  });

  it("error 非空时渲染错误条", () => {
    render(
      <WorkflowEditorForm
        name="x"
        content=""
        error="boom"
        onNameChange={() => {}}
        onContentChange={() => {}}
      />,
    );
    expect(screen.getByText("boom")).toBeTruthy();
  });
});
