import {
  Beaker,
  Bot,
  Brain,
  Brush,
  Bug,
  LineChart,
  CodeXml,
  Compass,
  Cpu,
  Database,
  Eye,
  Flag,
  Gauge,
  Hammer,
  Layers,
  Megaphone,
  Network,
  PaintBucket,
  Palette,
  Package,
  Puzzle,
  Rocket,
  Ruler,
  Scale,
  ShieldCheck,
  Sparkles,
  Target,
  Terminal,
  WandSparkles,
  Wrench,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";

import i18n from "@/i18n";

export type IconCategory =
  | "engineering"
  | "design"
  | "ai"
  | "qa"
  | "ops"
  | "general";

export interface IconMeta {
  key: string;
  label: string;
  icon: LucideIcon;
  category: IconCategory;
  aliases?: string[];
}

export const ICON_CATEGORIES: { key: IconCategory; label: string }[] = [
  { key: "engineering", label: i18n.t("iconRegistry.categories.engineering") },
  { key: "design", label: i18n.t("iconRegistry.categories.design") },
  { key: "ai", label: "AI" },
  { key: "qa", label: i18n.t("iconRegistry.categories.qa") },
  { key: "ops", label: i18n.t("iconRegistry.categories.ops") },
  { key: "general", label: i18n.t("iconRegistry.categories.general") },
];

export const ICON_LIST: IconMeta[] = [
  // 工程
  {
    key: "hammer",
    label: i18n.t("iconRegistry.icons.hammer"),
    icon: Hammer,
    category: "engineering",
    aliases: ["build", i18n.t("iconRegistry.aliases.construction")],
  },
  {
    key: "code-xml",
    label: i18n.t("iconRegistry.icons.code"),
    icon: CodeXml,
    category: "engineering",
    aliases: ["code", i18n.t("iconRegistry.aliases.coding")],
  },
  {
    key: "terminal",
    label: i18n.t("iconRegistry.icons.terminal"),
    icon: Terminal,
    category: "engineering",
    aliases: ["cli", "shell"],
  },
  {
    key: "wrench",
    label: i18n.t("iconRegistry.icons.wrench"),
    icon: Wrench,
    category: "engineering",
    aliases: ["tools", i18n.t("iconRegistry.aliases.tools")],
  },
  {
    key: "cpu",
    label: i18n.t("iconRegistry.icons.cpu"),
    icon: Cpu,
    category: "engineering",
    aliases: ["chip", i18n.t("iconRegistry.aliases.hardware")],
  },
  {
    key: "network",
    label: i18n.t("iconRegistry.icons.network"),
    icon: Network,
    category: "engineering",
    aliases: ["net", i18n.t("iconRegistry.aliases.topology")],
  },

  // 设计
  {
    key: "palette",
    label: i18n.t("iconRegistry.icons.palette"),
    icon: Palette,
    category: "design",
    aliases: ["color", i18n.t("iconRegistry.aliases.color")],
  },
  {
    key: "brush",
    label: i18n.t("iconRegistry.icons.brush"),
    icon: Brush,
    category: "design",
    aliases: ["paint", i18n.t("iconRegistry.aliases.painting")],
  },
  {
    key: "paint-bucket",
    label: i18n.t("iconRegistry.icons.paintBucket"),
    icon: PaintBucket,
    category: "design",
    aliases: ["fill", i18n.t("iconRegistry.aliases.fill")],
  },
  {
    key: "layers",
    label: i18n.t("iconRegistry.icons.layers"),
    icon: Layers,
    category: "design",
    aliases: ["stack", i18n.t("iconRegistry.aliases.hierarchy")],
  },
  {
    key: "ruler",
    label: i18n.t("iconRegistry.icons.ruler"),
    icon: Ruler,
    category: "design",
    aliases: ["measure", i18n.t("iconRegistry.aliases.measure")],
  },

  // AI
  {
    key: "sparkles",
    label: i18n.t("iconRegistry.icons.sparkles"),
    icon: Sparkles,
    category: "ai",
    aliases: ["ai", i18n.t("iconRegistry.aliases.intelligence")],
  },
  {
    key: "brain",
    label: i18n.t("iconRegistry.icons.brain"),
    icon: Brain,
    category: "ai",
    aliases: ["think", i18n.t("iconRegistry.aliases.thinking")],
  },
  {
    key: "bot",
    label: i18n.t("iconRegistry.icons.bot"),
    icon: Bot,
    category: "ai",
    aliases: ["robot"],
  },
  {
    key: "wand-sparkles",
    label: i18n.t("iconRegistry.icons.wandSparkles"),
    icon: WandSparkles,
    category: "ai",
    aliases: ["magic", i18n.t("iconRegistry.aliases.magic")],
  },

  // 测试
  {
    key: "beaker",
    label: i18n.t("iconRegistry.icons.beaker"),
    icon: Beaker,
    category: "qa",
    aliases: ["lab", i18n.t("iconRegistry.aliases.experiment")],
  },
  {
    key: "bug",
    label: "Bug",
    icon: Bug,
    category: "qa",
    aliases: ["debug", i18n.t("iconRegistry.aliases.debugging")],
  },
  {
    key: "shield-check",
    label: i18n.t("iconRegistry.icons.security"),
    icon: ShieldCheck,
    category: "qa",
    aliases: ["security", i18n.t("iconRegistry.aliases.protection")],
  },
  {
    key: "scale",
    label: i18n.t("iconRegistry.icons.scale"),
    icon: Scale,
    category: "qa",
    aliases: ["balance", i18n.t("iconRegistry.aliases.tradeoff")],
  },

  // 运维
  {
    key: "rocket",
    label: i18n.t("iconRegistry.icons.rocket"),
    icon: Rocket,
    category: "ops",
    aliases: ["launch", i18n.t("iconRegistry.aliases.release")],
  },
  {
    key: "gauge",
    label: i18n.t("iconRegistry.icons.gauge"),
    icon: Gauge,
    category: "ops",
    aliases: ["metric", i18n.t("iconRegistry.aliases.monitoring")],
  },
  {
    key: "database",
    label: i18n.t("iconRegistry.icons.database"),
    icon: Database,
    category: "ops",
    aliases: ["db", i18n.t("iconRegistry.aliases.storage")],
  },
  {
    key: "chart-line",
    label: i18n.t("iconRegistry.icons.chart"),
    icon: LineChart,
    category: "ops",
    aliases: ["chart", i18n.t("iconRegistry.aliases.trend")],
  },
  {
    key: "package",
    label: i18n.t("iconRegistry.icons.package"),
    icon: Package,
    category: "ops",
    aliases: ["bundle", i18n.t("iconRegistry.aliases.artifact")],
  },

  // 通用
  {
    key: "puzzle",
    label: i18n.t("iconRegistry.icons.puzzle"),
    icon: Puzzle,
    category: "general",
    aliases: ["module", i18n.t("iconRegistry.aliases.module")],
  },
  {
    key: "target",
    label: i18n.t("iconRegistry.icons.target"),
    icon: Target,
    category: "general",
    aliases: ["goal", i18n.t("iconRegistry.aliases.focus")],
  },
  {
    key: "flag",
    label: i18n.t("iconRegistry.icons.flag"),
    icon: Flag,
    category: "general",
    aliases: ["mark", i18n.t("iconRegistry.aliases.milestone")],
  },
  {
    key: "compass",
    label: i18n.t("iconRegistry.icons.compass"),
    icon: Compass,
    category: "general",
    aliases: ["direction", i18n.t("iconRegistry.aliases.direction")],
  },
  {
    key: "megaphone",
    label: i18n.t("iconRegistry.icons.megaphone"),
    icon: Megaphone,
    category: "general",
    aliases: ["announce", i18n.t("iconRegistry.aliases.notification")],
  },
  {
    key: "eye",
    label: i18n.t("iconRegistry.icons.eye"),
    icon: Eye,
    category: "general",
    aliases: ["watch", i18n.t("iconRegistry.aliases.observe")],
  },
];

export const ICON_BY_KEY: Map<string, IconMeta> = new Map(
  ICON_LIST.map((m) => [m.key, m]),
);

export function iconForKey(key: string | null | undefined): LucideIcon {
  if (!key) return Puzzle;
  return ICON_BY_KEY.get(key)?.icon ?? Puzzle;
}

export function hasIcon(key: string | null | undefined): boolean {
  if (!key) return false;
  return ICON_BY_KEY.has(key);
}

export function searchIcons(query: string): IconMeta[] {
  const q = query.trim().toLowerCase();
  if (!q) return ICON_LIST;
  return ICON_LIST.filter((m) => {
    if (m.key.toLowerCase().includes(q)) return true;
    if (m.label.toLowerCase().includes(q)) return true;
    if (m.aliases?.some((a) => a.toLowerCase().includes(q))) return true;
    return false;
  });
}

export function iconsByCategory(): {
  category: IconCategory;
  label: string;
  items: IconMeta[];
}[] {
  return ICON_CATEGORIES.map((c) => ({
    category: c.key,
    label: c.label,
    items: ICON_LIST.filter((m) => m.category === c.key),
  }));
}
