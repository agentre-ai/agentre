import { describe, expect, it } from "vitest";

import { reasonToDisplayStatus, reasonToPillText } from "../attention-display";
import type { AttentionReason } from "@/stores/attention-store";

describe("reasonToDisplayStatus", () => {
  it("needs_attention / unread → waiting", () => {
    expect(reasonToDisplayStatus("needs_attention", "idle")).toBe("waiting");
    expect(reasonToDisplayStatus("unread", "idle")).toBe("waiting");
  });
  it("running → running", () => {
    expect(reasonToDisplayStatus("running", "idle")).toBe("running");
  });
  it("error → error", () => {
    expect(reasonToDisplayStatus("error", "idle")).toBe("error");
  });
  it("null → fallback", () => {
    expect(reasonToDisplayStatus(null, "running")).toBe("running");
    expect(reasonToDisplayStatus(null, "idle")).toBe("idle");
  });
});

describe("reasonToPillText", () => {
  it.each<[AttentionReason, string | null]>([
    ["needs_attention", "Approval"],
    ["error", "Error"],
    ["unread", "Unread"],
    ["running", null],
  ])("%s → %s", (reason, expected) => {
    expect(reasonToPillText(reason)).toBe(expected);
  });
  it("null → null", () => {
    expect(reasonToPillText(null)).toBeNull();
  });
});

import i18n from "@/i18n";

it("maps bg_running to the running display color", () => {
  expect(reasonToDisplayStatus("bg_running", "idle")).toBe("running");
});
it("pill text for bg_running is the background label", () => {
  expect(reasonToPillText("bg_running")).toBe(i18n.t("attention.background"));
});
