import { describe, expect, it } from "vitest";
import { groupCatalogItems, type CatalogItem } from "../catalog";

const item = (id: string, group: string): CatalogItem => ({
  id,
  name: id,
  description: "",
  group,
  enabled: false,
});

describe("groupCatalogItems", () => {
  it("groups by group label preserving first-seen order", () => {
    const groups = groupCatalogItems([
      item("a", "推荐"),
      item("b", "已安装"),
      item("c", "推荐"),
    ]);
    expect(groups.map((g) => g.group)).toEqual(["推荐", "已安装"]);
    expect(groups[0].items.map((i) => i.id)).toEqual(["a", "c"]);
    expect(groups[1].items.map((i) => i.id)).toEqual(["b"]);
  });

  it("puts items without a group under empty key in trailing order", () => {
    const groups = groupCatalogItems([item("a", ""), item("b", "推荐")]);
    expect(groups.map((g) => g.group)).toEqual(["", "推荐"]);
  });
});
