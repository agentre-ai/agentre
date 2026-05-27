import { beforeEach, describe, expect, it } from "vitest";

import {
  clearLastContext,
  readLastContext,
  writeLastContext,
} from "../new-chat-context-persistence";

describe("new-chat-context-persistence", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  describe("readLastContext / writeLastContext", () => {
    it("round-trips a context", () => {
      writeLastContext({
        projectID: 7,
        projectName: "后端重构",
      });
      expect(readLastContext()).toEqual({
        projectID: 7,
        projectName: "后端重构",
      });
    });

    it("clearLastContext removes the entry", () => {
      writeLastContext({
        projectID: 1,
        projectName: "x",
      });
      clearLastContext();
      expect(readLastContext()).toBeNull();
    });

    it("returns null when nothing is stored", () => {
      expect(readLastContext()).toBeNull();
    });

    it("returns null on invalid data", () => {
      localStorage.setItem("agentre.commandPalette.lastContext", "garbage");
      expect(readLastContext()).toBeNull();
    });
  });
});
