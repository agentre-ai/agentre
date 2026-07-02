import { describe, expect, it } from "vitest";

import { bucketByPlacement } from "../reorder";
import type { OrgAgent } from "../types";

describe("bucketByPlacement", () => {
  it("按原始 placement 分桶并保持相对序", () => {
    const byId = new Map<number, OrgAgent>([
      [1, { id: 1, departmentId: 2, parentAgentId: 0 } as OrgAgent],
      [2, { id: 2, departmentId: 2, parentAgentId: 0 } as OrgAgent],
      [3, { id: 3, departmentId: 0, parentAgentId: 5 } as OrgAgent],
    ]);
    const buckets = bucketByPlacement(byId, [2, 3, 1]);
    const dept2 = buckets.find((b) => b.departmentId === 2)!;
    const parent5 = buckets.find((b) => b.parentAgentId === 5)!;
    expect(dept2.orderedIds).toEqual([2, 1]); // 相对序:2 在 1 前
    expect(parent5.orderedIds).toEqual([3]);
  });
});
