import { describe, expect, it } from "vitest";
import i18n from "@/i18n";

import { skillPacksToCatalog } from "../skill-catalog";

const t = i18n.getFixedT("en");
const pack = (over: Record<string, unknown> = {}) => ({
  id: "x@m",
  name: "x",
  description: "d",
  skills: ["a", "b"],
  source: "installed",
  recommended: false,
  installed: true,
  enabled: false,
  globallyEnabled: false,
  ...over,
});

describe("skillPacksToCatalog", () => {
  it("globally-enabled pack → inherited group, tri-state, with global-enabled note", () => {
    const [it0] = skillPacksToCatalog(
      [pack({ installed: true, globallyEnabled: true })],
      t,
    );
    expect(it0.group).toBe(t("org.agent.skillCatalog.group.inheritedOn"));
    expect(it0.state).toBe("inherit");
    expect(it0.globallyEnabled).toBe(true);
    expect(it0.description).toContain(
      t("org.agent.skillCatalog.globalEnabled"),
    );
    expect(it0.disabledReason).toBeUndefined();
  });

  it("installed but globally-off pack → enableable group", () => {
    const [it0] = skillPacksToCatalog(
      [pack({ installed: true, globallyEnabled: false })],
      t,
    );
    expect(it0.group).toBe(t("org.agent.skillCatalog.group.enableable"));
    expect(it0.state).toBe("inherit");
    expect(it0.description).toContain(
      t("org.agent.skillCatalog.globalDisabled"),
    );
    expect(it0.disabledReason).toBeUndefined();
  });

  it("not-installed pack → available group, disabled with needInstall", () => {
    const [it0] = skillPacksToCatalog(
      [pack({ source: "available", installed: false, globallyEnabled: false })],
      t,
    );
    expect(it0.group).toBe(t("org.agent.skillCatalog.group.available"));
    expect(it0.disabledReason).toBe(t("org.agent.skillCatalog.needInstall"));
    expect(it0.state).toBeUndefined();
  });
});
