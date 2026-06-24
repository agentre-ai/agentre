import { describe, it, expect } from "vitest";
import { ORCH_EVENTS } from "../events";

describe("ORCH_EVENTS", () => {
  it("与后端事件名一致", () => {
    expect(ORCH_EVENTS.updated).toBe("orch:run:updated");
    expect(Object.values(ORCH_EVENTS)).toContain("orch:run:deadlock");
  });
});
