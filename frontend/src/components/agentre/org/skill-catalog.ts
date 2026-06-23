import type { TFunction } from "i18next";

import type { CatalogItem } from "../capability/catalog";
import type { skill_svc } from "../../../../wailsjs/go/models";

// skillPacksToCatalog 把后端 SkillPackDTO[] 适配为 CapabilityPicker 的 CatalogItem[]。
// 分组按"继承(全局已开)/ 可启用(已安装·全局未开)/ 可安装(未装)"。
// 已安装的行给 state（三态由 org-detail 用本地授权 overlay 覆盖），未安装行禁用。
export function skillPacksToCatalog(
  packs: skill_svc.SkillPackDTO[],
  t: TFunction,
): CatalogItem[] {
  return packs.map((p) => {
    const globalNote = p.globallyEnabled
      ? t("org.agent.skillCatalog.globalEnabled")
      : t("org.agent.skillCatalog.globalDisabled");
    const group = !p.installed
      ? t("org.agent.skillCatalog.group.available")
      : p.globallyEnabled
        ? t("org.agent.skillCatalog.group.inheritedOn")
        : t("org.agent.skillCatalog.group.enableable");
    return {
      id: p.id,
      name: p.name,
      description: p.installed
        ? `${p.description} · ${globalNote}`
        : p.description,
      contents: p.skills ?? [],
      group,
      enabled: p.enabled,
      globallyEnabled: p.globallyEnabled,
      state: p.installed ? "inherit" : undefined,
      disabledReason: p.installed
        ? undefined
        : t("org.agent.skillCatalog.needInstall"),
    };
  });
}
