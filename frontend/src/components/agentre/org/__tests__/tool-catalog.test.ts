import { describe, expect, it } from "vitest";
import i18n from "@/i18n";
import { toolKeysToCatalog } from "../tool-catalog";

const t = i18n.getFixedT("en");

describe("toolKeysToCatalog", () => {
  it("maps tool keys with localized names + approval badge for org", () => {
    const items = toolKeysToCatalog(
      ["org"],
      [{ key: "org", enabled: true }],
      t,
    );
    expect(items[0].id).toBe("org");
    expect(items[0].name).toBe(t("org.agent.tools.names.org"));
    expect(items[0].enabled).toBe(true);
    expect(items[0].badges?.[0]?.tone).toBe("approval");
  });

  it("marks unknown agent tools as not enabled", () => {
    const items = toolKeysToCatalog(["org"], [], t);
    expect(items[0].enabled).toBe(false);
  });

  it("workflow 带审批徽标 + 名称来自 i18n", () => {
    const items = toolKeysToCatalog(
      ["org", "workflow"],
      [{ key: "workflow", enabled: true }],
      t,
    );
    const wf = items.find((i) => i.id === "workflow")!;
    expect(wf.name).toBe(t("org.agent.tools.names.workflow"));
    expect(wf.enabled).toBe(true);
    expect(wf.badges?.[0]?.tone).toBe("approval");
  });

  it("group_create 带审批徽标 + enabled 取自 agentTools", () => {
    const items = toolKeysToCatalog(
      ["org", "workflow", "group_create"],
      [{ key: "group_create", enabled: true }],
      t,
    );
    const gc = items.find((i) => i.id === "group_create")!;
    expect(gc.name).toBe(t("org.agent.tools.names.group_create"));
    expect(gc.enabled).toBe(true);
    expect(gc.badges?.[0]?.tone).toBe("approval");
  });
});
