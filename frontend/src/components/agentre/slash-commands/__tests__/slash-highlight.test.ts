import { describe, expect, it } from "vitest";

import { findValidSlashRanges } from "../slash-highlight";

// findValidSlashRanges 在段落纯文本里找出 token 完整等于 validNames 中某项的
// /command 区间。边界规则与 detectSlashTrigger 一致：左侧行首或空白；右侧行尾
// 或空白；token 字符 [a-zA-Z][a-zA-Z0-9_-]*。
//
// 大小写敏感(与 popover filterByQuery 不同):popover 是“补全建议”可以模糊,
// 高亮是“已确认是这个命令”，必须严格等于注册名。
describe("findValidSlashRanges", () => {
  const valid = new Set(["compact"]);

  it("完整匹配命中：/compact 整段亮", () => {
    expect(findValidSlashRanges("/compact", valid)).toEqual([
      { from: 0, to: 8 },
    ]);
  });

  it("不完整不亮：/compac 比注册名短", () => {
    expect(findValidSlashRanges("/compac", valid)).toEqual([]);
  });

  it("token 后接其他字母不亮：/compactx", () => {
    expect(findValidSlashRanges("/compactx", valid)).toEqual([]);
  });

  it("词内 / 不当作命令：/foo/compact", () => {
    expect(findValidSlashRanges("/foo/compact", valid)).toEqual([]);
  });

  it("命令后跟空格 + 参数：只亮 /compact 部分", () => {
    expect(findValidSlashRanges("/compact arg", valid)).toEqual([
      { from: 0, to: 8 },
    ]);
  });

  it("同段落两次命中：返回两个 range", () => {
    expect(findValidSlashRanges("/compact /compact", valid)).toEqual([
      { from: 0, to: 8 },
      { from: 9, to: 17 },
    ]);
  });

  it("validNames 为空集 → 空", () => {
    expect(findValidSlashRanges("/compact", new Set())).toEqual([]);
  });

  it("未注册命令不亮：/unknown", () => {
    expect(findValidSlashRanges("/unknown", valid)).toEqual([]);
  });

  it("大小写敏感：/Compact 不亮", () => {
    expect(findValidSlashRanges("/Compact", valid)).toEqual([]);
  });

  it("命令前导文字：hello /compact", () => {
    expect(findValidSlashRanges("hello /compact", valid)).toEqual([
      { from: 6, to: 14 },
    ]);
  });

  it("命令前是 tab 也算空白边界", () => {
    expect(findValidSlashRanges("\t/compact", valid)).toEqual([
      { from: 1, to: 9 },
    ]);
  });

  it("空字符串 → 空", () => {
    expect(findValidSlashRanges("", valid)).toEqual([]);
  });

  it("仅一个 / 不算命令", () => {
    expect(findValidSlashRanges("/", valid)).toEqual([]);
  });
});
