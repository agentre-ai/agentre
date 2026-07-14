import { describe, expect, it } from "vitest";

import { splitErrorDetail } from "./error-detail";

describe("splitErrorDetail", () => {
  it("按首个换行把后端错误拆成 headline 与 detail", () => {
    const e = new Error(
      "操作失败\nSQL logic error: table chat_sessions has no column named run_id (1)",
    );

    expect(splitErrorDetail(e)).toEqual({
      msg: "操作失败",
      detail: "SQL logic error: table chat_sessions has no column named run_id (1)",
    });
  });

  it("无换行时 detail 为 undefined", () => {
    expect(splitErrorDetail(new Error("操作失败"))).toEqual({
      msg: "操作失败",
      detail: undefined,
    });
  });

  it("cause 自身含换行时只按首个换行拆,余下整体留在 detail", () => {
    const e = new Error("操作失败\nline1\nline2");

    expect(splitErrorDetail(e)).toEqual({
      msg: "操作失败",
      detail: "line1\nline2",
    });
  });

  it("非 Error 值退回 String(e)", () => {
    expect(splitErrorDetail("boom")).toEqual({ msg: "boom", detail: undefined });
  });

  it("detail 两侧空白被裁掉;裁完为空则视作无 detail", () => {
    expect(splitErrorDetail(new Error("操作失败\n   "))).toEqual({
      msg: "操作失败",
      detail: undefined,
    });
  });
});
