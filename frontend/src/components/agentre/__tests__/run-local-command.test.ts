import { describe, it, expect } from "vitest";
import { makeStreamDecoder } from "../local-command/decode";

describe("makeStreamDecoder", () => {
  it("decodes base64 chunks incrementally", () => {
    const dec = makeStreamDecoder();
    // "hi" = aGk=
    expect(dec("aGk=")).toBe("hi");
  });
});
