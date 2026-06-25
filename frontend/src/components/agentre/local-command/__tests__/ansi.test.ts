import { it, expect } from "vitest";
import { stripAnsi } from "../ansi";
it("strips SGR escape sequences", () => {
  expect(stripAnsi("[32mPASS[0m ok")).toBe("PASS ok");
});
