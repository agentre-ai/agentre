import { describe, expect, it } from "vitest";

import { moveItem } from "../exec-target-reorder";

describe("moveItem", () => {
  it("moves an item earlier in the list", () => {
    expect(moveItem([1, 2, 3], 2, 0)).toEqual([3, 1, 2]);
  });

  it("moves an item later in the list", () => {
    expect(moveItem([1, 2, 3], 0, 2)).toEqual([2, 3, 1]);
  });

  it("is a no-op when from === to (same reference)", () => {
    const list = [1, 2, 3];
    expect(moveItem(list, 1, 1)).toBe(list);
  });

  it("is a no-op when from is out of range (same reference)", () => {
    const list = [1, 2, 3];
    expect(moveItem(list, -1, 0)).toBe(list);
    expect(moveItem(list, 3, 0)).toBe(list);
  });

  it("is a no-op when to is out of range (same reference)", () => {
    const list = [1, 2, 3];
    expect(moveItem(list, 0, -1)).toBe(list);
    expect(moveItem(list, 0, 3)).toBe(list);
  });

  it("does not mutate the input array", () => {
    const list = [1, 2, 3];
    moveItem(list, 0, 2);
    expect(list).toEqual([1, 2, 3]);
  });
});
