import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";

import { MarkdownText } from "../../../markdown-text";
import { makeMentionDecorator, prepareMentionText } from "../transcript";

describe("prepareMentionText", () => {
  it("replaces tags with sentinels and collects refs", () => {
    const { text, refs } = prepareMentionText(
      'hi <agent id="12">Reviewer</agent> and <project id="3" path="/w">Web</project>',
    );
    expect(text).toBe("hi 0 and 1");
    expect(refs).toEqual([
      { kind: "agent", refId: 12, label: "Reviewer" },
      { kind: "project", refId: 3, label: "Web", path: "/w" },
    ]);
  });

  it("leaves plain text untouched with empty refs", () => {
    expect(prepareMentionText("just text")).toEqual({
      text: "just text",
      refs: [],
    });
  });
});

describe("MarkdownText + mention decorator", () => {
  it("renders a chip for the mention sentinel", () => {
    const { text, refs } = prepareMentionText('yo <agent id="1">Bob</agent>');
    render(
      <MemoryRouter>
        <MarkdownText text={text} decorator={makeMentionDecorator(refs)} />
      </MemoryRouter>,
    );
    expect(screen.getByText("@Bob")).toBeInTheDocument();
  });
});
