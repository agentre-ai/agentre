import { describe, expect, it } from "vitest";

import { afterIdForDrop, groupByStage } from "../reorder";

const mk = (id: number, stage: string, position: number) =>
  ({ id, stage, position, labels: [] }) as any;

describe("groupByStage", () => {
  it("按 stage 分组且列内按 position 升序", () => {
    const g = groupByStage([mk(2, "todo", 20), mk(1, "todo", 10), mk(3, "done", 5)]);
    expect(g.todo.map((i) => i.id)).toEqual([1, 2]);
    expect(g.done.map((i) => i.id)).toEqual([3]);
    expect(g.doing).toEqual([]);
  });
});

describe("afterIdForDrop", () => {
  const list = [mk(1, "todo", 10), mk(2, "todo", 20)];
  it("放到索引 0 → afterID=0（顶部）", () => {
    expect(afterIdForDrop(list, 0)).toBe(0);
  });
  it("放到索引 1 → afterID=前一张卡 id", () => {
    expect(afterIdForDrop(list, 1)).toBe(1);
  });
  it("放到末尾 → afterID=最后一张卡 id", () => {
    expect(afterIdForDrop(list, 2)).toBe(2);
  });
});
