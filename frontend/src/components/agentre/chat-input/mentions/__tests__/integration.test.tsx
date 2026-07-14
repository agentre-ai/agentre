import { act, render, screen, waitFor } from "@testing-library/react";
import { useRef } from "react";
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

function Harness({ onSubmit }: { onSubmit: (t: string) => void }) {
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
        autoFocus
      />
    </>
  );
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
});
