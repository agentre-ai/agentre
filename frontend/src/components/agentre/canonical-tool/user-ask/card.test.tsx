import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";

import { UserAskCard } from "./card";
import type { ChatBlockData } from "@/stores/chat-streams-store";

vi.mock("../../../../../wailsjs/go/app/App", () => ({
  AnswerUserQuestion: vi.fn().mockResolvedValue(undefined),
}));

describe("UserAskCard", () => {
  it("renders nothing without canonical", () => {
    const block = {
      type: "tool_use",
      toolName: "AskUserQuestion",
    } as unknown as ChatBlockData;
    const { container } = render(
      <UserAskCard toolBlock={block} sessionId={1} />,
    );
    expect(container.firstChild).toBeNull();
  });

  it("renders question + options + WAITING pill", () => {
    const block = {
      type: "tool_use",
      toolName: "AskUserQuestion",
      canonical: {
        kind: "user.ask",
        userAsk: {
          requestId: "req-1",
          questions: [
            {
              question: "想用哪种方式?",
              header: "选项",
              options: [
                { label: "A", description: "" },
                { label: "B", description: "" },
              ],
            },
          ],
        },
      },
    } as unknown as ChatBlockData;
    render(<UserAskCard toolBlock={block} sessionId={1} />);
    expect(screen.getByText("想用哪种方式?")).toBeDefined();
    expect(screen.getByText("A")).toBeDefined();
    expect(screen.getByText(/WAITING/)).toBeDefined();
  });

  it("renders ANSWERED state when answered", () => {
    const block = {
      type: "tool_use",
      toolName: "AskUserQuestion",
      canonical: {
        kind: "user.ask",
        userAsk: {
          requestId: "req-1",
          questions: [
            {
              question: "?",
              header: "h",
              options: [{ label: "A", description: "" }],
            },
          ],
          answers: [{ questionIndex: 0, labels: ["A"] }],
          answered: true,
        },
      },
    } as unknown as ChatBlockData;
    render(<UserAskCard toolBlock={block} sessionId={1} />);
    expect(screen.getByText("ANSWERED")).toBeDefined();
  });

  it("Given answered multiple questions, When switching question tabs, Then answers remain reviewable but locked", async () => {
    const user = userEvent.setup();
    const block = {
      type: "tool_use",
      toolName: "AskUserQuestion",
      canonical: {
        kind: "user.ask",
        userAsk: {
          requestId: "req-1",
          questions: [
            {
              question: "First question?",
              header: "First",
              options: [{ label: "A", description: "" }],
            },
            {
              question: "Second question?",
              header: "Second",
              options: [{ label: "B", description: "" }],
            },
          ],
          answers: [
            { questionIndex: 0, labels: ["A"] },
            { questionIndex: 1, labels: ["B"] },
          ],
          answered: true,
        },
      },
    } as unknown as ChatBlockData;

    render(<UserAskCard toolBlock={block} sessionId={1} />);

    await user.click(screen.getByRole("button", { name: /Q2 · Second/ }));

    expect(screen.getByText("Second question?")).toBeDefined();
    expect(screen.getByRole("button", { name: /^B$/ })).toBeDisabled();
    expect(screen.getByRole("textbox")).toBeDisabled();
  });

  it("Given skipped multiple questions, When switching question tabs, Then questions remain reviewable without answer actions", async () => {
    const user = userEvent.setup();
    const block = {
      type: "tool_use",
      toolName: "AskUserQuestion",
      canonical: {
        kind: "user.ask",
        userAsk: {
          requestId: "req-1",
          questions: [
            {
              question: "First question?",
              header: "First",
              options: [{ label: "A", description: "" }],
            },
            {
              question: "Second question?",
              header: "Second",
              options: [{ label: "B", description: "" }],
            },
          ],
          skipped: true,
        },
      },
    } as unknown as ChatBlockData;

    render(<UserAskCard toolBlock={block} sessionId={1} />);

    await user.click(screen.getByRole("button", { name: /Q2 · Second/ }));

    expect(screen.getByText("Second question?")).toBeDefined();
    expect(screen.getByRole("button", { name: /^B$/ })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Submit" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Skip" })).toBeNull();
  });
});
