import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { SuggestionPopover } from "../suggestion-popover";

describe("SuggestionPopover viewport anchoring", () => {
  it("Given the composer is inside an offset clipping container, When suggestions open, Then the floating layer stays in the viewport coordinate space", () => {
    render(
      <div data-testid="composer-container" className="overflow-hidden">
        <SuggestionPopover
          open
          anchorRect={{ left: 67, top: 404, bottom: 424 }}
          selectedIndex={0}
          itemCount={1}
          ariaLabel="Suggestions"
        >
          {(activeRef) => (
            <button ref={activeRef} role="option" type="button">
              Suggestion
            </button>
          )}
        </SuggestionPopover>
      </div>,
    );

    const listbox = screen.getByRole("listbox", { name: "Suggestions" });
    expect(listbox.parentElement).toBe(document.body);
    expect(listbox).toHaveStyle({
      position: "fixed",
      left: "67px",
      bottom: `${window.innerHeight - 404 + 4}px`,
    });
  });
});
