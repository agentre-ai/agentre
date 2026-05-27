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
  { key: "engineering", label: "工程" },
  { key: "design", label: "设计" },
  { key: "ai", label: "AI" },
  { key: "qa", label: "测试" },
  { key: "ops", label: "运维" },
  { key: "general", label: "通用" },
];

export const ICON_LIST: IconMeta[] = [
  // 工程
  {
    key: "hammer",
    label: "锤子",
    icon: Hammer,
    category: "engineering",
    aliases: ["build", "施工"],
  },
  {
    key: "code-xml",
    label: "代码",
    icon: CodeXml,
    category: "engineering",
    aliases: ["code", "编码"],
  },
  {
    key: "terminal",
    label: "终端",
    icon: Terminal,
    category: "engineering",
    aliases: ["cli", "shell"],
  },
  {
    key: "wrench",
    label: "扳手",
    icon: Wrench,
    category: "engineering",
    aliases: ["tools", "工具"],
  },
  {
    key: "cpu",
    label: "处理器",
    icon: Cpu,
    category: "engineering",
    aliases: ["chip", "硬件"],
  },
  {
    key: "network",
    label: "网络",
    icon: Network,
    category: "engineering",
    aliases: ["net", "拓扑"],
  },

  // 设计
  {
    key: "palette",
    label: "调色板",
    icon: Palette,
    category: "design",
    aliases: ["color", "配色"],
  },
  {
    key: "brush",
    label: "画笔",
    icon: Brush,
    category: "design",
    aliases: ["paint", "绘画"],
  },
  {
    key: "paint-bucket",
    label: "油漆桶",
    icon: PaintBucket,
    category: "design",
    aliases: ["fill", "填色"],
  },
  {
    key: "layers",
    label: "图层",
    icon: Layers,
    category: "design",
    aliases: ["stack", "层级"],
  },
  {
    key: "ruler",
    label: "尺子",
    icon: Ruler,
    category: "design",
    aliases: ["measure", "测量"],
  },

  // AI
  {
    key: "sparkles",
    label: "灵感",
    icon: Sparkles,
    category: "ai",
    aliases: ["ai", "智能"],
  },
  {
    key: "brain",
    label: "大脑",
    icon: Brain,
    category: "ai",
    aliases: ["think", "思考"],
  },
  {
    key: "bot",
    label: "机器人",
    icon: Bot,
    category: "ai",
    aliases: ["robot"],
  },
  {
    key: "wand-sparkles",
    label: "魔法棒",
    icon: WandSparkles,
    category: "ai",
    aliases: ["magic", "魔法"],
  },

  // 测试
  {
    key: "beaker",
    label: "烧杯",
    icon: Beaker,
    category: "qa",
    aliases: ["lab", "实验"],
  },
  {
    key: "bug",
    label: "Bug",
    icon: Bug,
    category: "qa",
    aliases: ["debug", "调试"],
  },
  {
    key: "shield-check",
    label: "安全",
    icon: ShieldCheck,
    category: "qa",
    aliases: ["security", "防护"],
  },
  {
    key: "scale",
    label: "天平",
    icon: Scale,
    category: "qa",
    aliases: ["balance", "权衡"],
  },

  // 运维
  {
    key: "rocket",
    label: "发射",
    icon: Rocket,
    category: "ops",
    aliases: ["launch", "发布"],
  },
  {
    key: "gauge",
    label: "仪表",
    icon: Gauge,
    category: "ops",
    aliases: ["metric", "监控"],
  },
  {
    key: "database",
    label: "数据库",
    icon: Database,
    category: "ops",
    aliases: ["db", "存储"],
  },
  {
    key: "chart-line",
    label: "图表",
    icon: LineChart,
    category: "ops",
    aliases: ["chart", "趋势"],
  },
  {
    key: "package",
    label: "包",
    icon: Package,
    category: "ops",
    aliases: ["bundle", "构件"],
  },

  // 通用
  {
    key: "puzzle",
    label: "拼图",
    icon: Puzzle,
    category: "general",
    aliases: ["module", "模块"],
  },
  {
    key: "target",
    label: "目标",
    icon: Target,
    category: "general",
    aliases: ["goal", "焦点"],
  },
  {
    key: "flag",
    label: "旗帜",
    icon: Flag,
    category: "general",
    aliases: ["mark", "里程碑"],
  },
  {
    key: "compass",
    label: "指南",
    icon: Compass,
    category: "general",
    aliases: ["direction", "方向"],
  },
  {
    key: "megaphone",
    label: "广播",
    icon: Megaphone,
    category: "general",
    aliases: ["announce", "通知"],
  },
  {
    key: "eye",
    label: "查看",
    icon: Eye,
    category: "general",
    aliases: ["watch", "观察"],
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
