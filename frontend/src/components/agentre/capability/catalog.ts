// CatalogItem 是「技能/工具添加弹窗」的统一契约（技能 pack 或工具 key）。
// 所有展示文案（name/description/group/badge.label/disabledReason）都应是
// 已本地化的字符串 —— 由消费者侧的映射函数用 t() 解析后填入，
// CapabilityPicker 只负责渲染，不再调 i18n。
export type TriState = "inherit" | "on" | "off";

export type CatalogBadgeTone =
  | "recommended"
  | "installed"
  | "available"
  | "approval"
  | "needInstall";

export type CatalogBadge = {
  label: string;
  tone: CatalogBadgeTone;
};

export type CatalogItem = {
  id: string; // pack id 或 tool key，全局唯一
  name: string;
  description: string;
  contents?: string[]; // pack 内 skill 名（可选，用于数量/展开）
  group: string; // 已本地化的分组标题（"" = 无分组）
  badges?: CatalogBadge[];
  enabled: boolean; // 当前是否已授予/勾选
  // 三态(技能用):存在 = 该行渲染「继承|开|关」分段控件而非单选框。
  state?: TriState;
  globallyEnabled?: boolean; // 全局是否已启用(用于分组/文案)
  disabledReason?: string; // 非空 = 该行禁用、不可勾选（如「需先安装」）
};

export type CatalogGroup = {
  group: string;
  items: CatalogItem[];
};

// groupCatalogItems 按 group 字段聚合，组顺序 = 组首次出现的顺序，
// 组内顺序 = 原始顺序。纯函数，便于表测。
export function groupCatalogItems(items: CatalogItem[]): CatalogGroup[] {
  const order: string[] = [];
  const byGroup = new Map<string, CatalogItem[]>();
  for (const item of items) {
    const key = item.group ?? "";
    if (!byGroup.has(key)) {
      byGroup.set(key, []);
      order.push(key);
    }
    byGroup.get(key)!.push(item);
  }
  return order.map((group) => ({ group, items: byGroup.get(group)! }));
}
