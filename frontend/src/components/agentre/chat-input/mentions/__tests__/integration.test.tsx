import { act, render, screen, waitFor } from "@testing-library/react";
import { useRef, type ComponentProps } from "react";
import { describe, expect, it, vi } from "vitest";

import type { Editor } from "@tiptap/react";

import { AIChatInput, type AIChatInputHandle } from "../../index";
import type { MentionSources } from "../types";

const sources: MentionSources = {
  agents: [{ kind: "agent", refId: 12, label: "Reviewer", color: "agent-3" }],
  projects: [
    { kind: "project", refId: 3, label: "Web", path: "/w", color: "agent-5" },
  ],
};

function Harness({
  onSubmit,
  backendType,
  onSlashSelect,
}: {
  onSubmit: (t: string) => void;
  backendType?: string;
  onSlashSelect?: ComponentProps<typeof AIChatInput>["onSlashSelect"];
}) {
  const editorRef = useRef<Editor | null>(null);
  const handleRef = useRef<AIChatInputHandle>(null);
  return (
    <>
      <button
        type="button"
        data-testid="ins"
        onClick={() => editorRef.current?.commands.insertContent("@")}
      >
        @
      </button>
      <button
        type="button"
        data-testid="ins-rev"
        onClick={() => editorRef.current?.commands.insertContent("Rev")}
      >
        Rev
      </button>
      <button
        type="button"
        data-testid="ins-slash"
        onClick={() => editorRef.current?.commands.insertContent("/")}
      >
        /
      </button>
      <button
        type="button"
        data-testid="submit"
        onClick={() => handleRef.current?.submit()}
      >
        submit
      </button>
      <AIChatInput
        ref={handleRef}
        onSubmit={onSubmit}
        editorRef={editorRef}
        mentionSources={sources}
        backendType={backendType}
        onSlashSelect={onSlashSelect}
        autoFocus
      />
    </>
  );
}

// 点 @ 按钮打开 mention 弹层 → 等 label 出现 → mousedown 选中 → 等弹层关闭。
async function pickMention(label: string): Promise<void> {
  act(() => screen.getByTestId("ins").click());
  await waitFor(() => screen.getByText(label));
  act(() =>
    screen
      .getByText(label)
      .closest("button")!
      .dispatchEvent(
        new MouseEvent("mousedown", { bubbles: true, cancelable: true }),
      ),
  );
  await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());
}

describe("AIChatInput @ mention integration", () => {
  it("@ opens grouped popover with agent + project", async () => {
    render(<Harness onSubmit={vi.fn()} />);
    act(() => screen.getByTestId("ins").click());
    await waitFor(() =>
      expect(screen.getByRole("listbox")).toBeInTheDocument(),
    );
    expect(screen.getByText("Reviewer")).toBeInTheDocument();
    expect(screen.getByText("Web")).toBeInTheDocument();
  });

  it("typing Rev filters to the agent only", async () => {
    render(<Harness onSubmit={vi.fn()} />);
    act(() => screen.getByTestId("ins").click());
    await waitFor(() => screen.getByText("Reviewer"));
    act(() => screen.getByTestId("ins-rev").click());
    await waitFor(() => expect(screen.queryByText("Web")).toBeNull());
    expect(screen.getByText("Reviewer")).toBeInTheDocument();
  });

  it("picking an agent inserts a chip that serializes to XML on submit", async () => {
    const onSubmit = vi.fn();
    render(<Harness onSubmit={onSubmit} />);
    act(() => screen.getByTestId("ins").click());
    await waitFor(() => screen.getByText("Reviewer"));
    act(() =>
      screen
        .getByText("Reviewer")
        .closest("button")!
        .dispatchEvent(
          new MouseEvent("mousedown", { bubbles: true, cancelable: true }),
        ),
    );
    await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());
    act(() => screen.getByTestId("submit").click());
    expect(onSubmit).toHaveBeenCalledWith(
      expect.stringContaining('<agent id="12">Reviewer</agent>'),
    );
  });

  // 回归:mention 是 inline atom 节点,textBetween 默认给它 0 字符但它在文档里
  // 仍占 1 个位置 —— 字符串下标与文档位置错位,deleteRange 会多吃掉前一个 chip。
  // 见 use-mention-menu.ts 的 leafText 注释。
  it("三个 mention 依次选中,提交时三个 chip 都还在(回归:atom 偏移错位吃掉中间 chip)", async () => {
    const onSubmit = vi.fn();
    render(<Harness onSubmit={onSubmit} />);

    await pickMention("Reviewer");
    await pickMention("Web");
    await pickMention("Reviewer");

    act(() => screen.getByTestId("submit").click());

    expect(onSubmit).toHaveBeenCalledTimes(1);
    const sent = onSubmit.mock.calls[0]?.[0] as string;
    expect(sent.match(/<agent id="12">Reviewer<\/agent>/g)).toHaveLength(2);
    expect(sent).toContain('<project id="3" path="/w">Web</project>');
  });

  // 回归:两个 chip 之后再选中 slash 命令,不应吃掉前面的 chip —— slash 模块和
  // mention 模块各自独立复现同一个 textBetween 偏移 bug,见 use-slash-menu.ts。
  it("插入两个 chip 后选中 /compact,提交时 chip 与 /compact 都还在(回归:atom 偏移错位吃掉 chip)", async () => {
    const onSubmit = vi.fn();
    render(
      <Harness
        onSubmit={onSubmit}
        backendType="claudecode"
        onSlashSelect={() => {}}
      />,
    );

    await pickMention("Reviewer");
    await pickMention("Web");

    act(() => screen.getByTestId("ins-slash").click());
    await waitFor(() => screen.getByText("/compact"));
    act(() =>
      screen
        .getByText("/compact")
        .closest("button")!
        .dispatchEvent(
          new MouseEvent("mousedown", { bubbles: true, cancelable: true }),
        ),
    );
    await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());

    act(() => screen.getByTestId("submit").click());

    expect(onSubmit).toHaveBeenCalledTimes(1);
    const sent = onSubmit.mock.calls[0]?.[0] as string;
    expect(sent).toContain('<agent id="12">Reviewer</agent>');
    expect(sent).toContain('<project id="3" path="/w">Web</project>');
    expect(sent).toContain("/compact");
  });
});
