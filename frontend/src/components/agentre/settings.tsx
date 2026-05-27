import * as React from "react";
import {
  Bell,
  Cable,
  Cpu,
  Database,
  Info,
  Keyboard,
  Network,
  Server,
  Sparkles,
  SunMoon,
  Wrench,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";

import type { AppTheme, AppThemePreference } from "./chrome";
import { AgentBackendsPanel } from "./agent-backends";
import { DataBackupPanel } from "./data-backup";
import { RemoteDevicesPanel } from "./remote-devices/remote-devices-panel";
import { LlmProvidersPanel } from "./llm-providers";
import { SettingsProxyPanel } from "./settings-proxy";
import { KeyboardShortcutsPanel } from "./shortcuts";
import { UnderConstructionPage } from "./under-construction-page";
import { UpdateSection } from "./update-section";

type SettingsNavSection = {
  label: string;
  items: {
    id?: SettingsPageId;
    icon: LucideIcon;
    label: string;
  }[];
};

type SettingsPageId =
  | "agent-backend"
  | "appearance"
  | "remote-devices"
  | "data-backup"
  | "keyboard-shortcuts"
  | "llm-providers"
  | "local-proxy"
  | "mcp-servers"
  | "notifications"
  | "skills-tools"
  | "version-logs";

const settingsNavSections: SettingsNavSection[] = [
  {
    label: "通用",
    items: [
      { icon: SunMoon, id: "appearance", label: "外观" },
      { icon: Bell, id: "notifications", label: "通知" },
      { icon: Keyboard, id: "keyboard-shortcuts", label: "键盘快捷键" },
      { icon: Database, id: "data-backup", label: "数据 & 备份" },
    ],
  },
  {
    label: "引擎",
    items: [
      { icon: Sparkles, id: "llm-providers", label: "LLM 供应商" },
      { icon: Cpu, id: "agent-backend", label: "Agent 后端" },
    ],
  },
  {
    label: "集成",
    items: [
      { icon: Network, id: "local-proxy", label: "本地 HTTP 代理" },
      { icon: Server, id: "mcp-servers", label: "MCP 服务器" },
      { icon: Wrench, id: "skills-tools", label: "技能 / 工具" },
      { icon: Cable, id: "remote-devices", label: "远端" },
    ],
  },
  {
    label: "关于",
    items: [{ icon: Info, id: "version-logs", label: "版本 & 更新" }],
  },
];

const underConstructionSettingsPages: Record<
  Exclude<
    SettingsPageId,
    | "agent-backend"
    | "appearance"
    | "remote-devices"
    | "keyboard-shortcuts"
    | "llm-providers"
    | "local-proxy"
    | "version-logs"
    | "data-backup"
  >,
  {
    description: string;
    icon: LucideIcon;
    title: string;
  }
> = {
  "mcp-servers": {
    title: "MCP 服务器",
    description:
      "MCP 服务器配置视图已预留，后续会在这里管理连接、授权和可用工具。",
    icon: Server,
  },
  notifications: {
    title: "通知",
    description: "通知设置视图已预留，后续会在这里调整提醒规则与通知触达渠道。",
    icon: Bell,
  },
  "skills-tools": {
    title: "技能 / 工具",
    description:
      "技能和工具管理视图已预留，后续会在这里启用、停用和配置 Agent 能力。",
    icon: Wrench,
  },
};

const compactSettingsNavItems = settingsNavSections
  .flatMap((section) => section.items)
  .filter((item): item is typeof item & { id: SettingsPageId } =>
    Boolean(item.id),
  );

type SettingsNavProps = {
  activePage: SettingsPageId;
  onPageChange: (page: SettingsPageId) => void;
};

function canUseMatchMedia() {
  return (
    typeof window !== "undefined" && typeof window.matchMedia === "function"
  );
}

function useMediaQuery(query: string) {
  return React.useSyncExternalStore(
    React.useCallback(
      (onStoreChange) => {
        if (!canUseMatchMedia()) {
          return () => {};
        }

        const mediaQuery = window.matchMedia(query);

        mediaQuery.addEventListener("change", onStoreChange);

        return () => {
          mediaQuery.removeEventListener("change", onStoreChange);
        };
      },
      [query],
    ),
    () => (canUseMatchMedia() ? window.matchMedia(query).matches : false),
    () => false,
  );
}

type SettingsNavButtonProps = {
  activePage: SettingsPageId;
  item: {
    icon: LucideIcon;
    id?: SettingsPageId;
    label: string;
  };
  onPageChange: (page: SettingsPageId) => void;
};

function SettingsNavButton({
  activePage,
  item,
  onPageChange,
}: SettingsNavButtonProps) {
  const Icon = item.icon;
  const active = item.id === activePage;
  const pageId = item.id;

  return (
    <Button
      key={item.label}
      type="button"
      variant="ghost"
      aria-current={active ? "page" : undefined}
      className={cn(
        "h-[30px] shrink-0 justify-start gap-2 px-2.5 text-sm font-normal whitespace-nowrap text-foreground lg:w-full",
        active &&
          "bg-primary-soft font-medium text-primary-text hover:bg-primary-soft hover:text-primary-text",
      )}
      onClick={pageId ? () => onPageChange(pageId) : undefined}
    >
      <Icon
        data-icon="inline-start"
        className={active ? "text-primary-text" : undefined}
        aria-hidden="true"
      />
      {item.label}
    </Button>
  );
}

function SettingsNav({ activePage, onPageChange }: SettingsNavProps) {
  const showFullNav = useMediaQuery("(min-width: 1024px)");

  return (
    <aside
      aria-label="设置导航"
      className="flex w-full shrink-0 flex-col gap-2 border-b border-border bg-sidebar px-3 py-3 lg:w-[220px] lg:gap-[18px] lg:border-b-0 lg:border-r lg:py-4"
    >
      <div className="px-1.5 text-sm font-semibold lg:pb-2">设置</div>
      <div className="flex flex-wrap gap-1.5 pb-1 lg:flex-col lg:flex-nowrap lg:gap-[18px] lg:p-0">
        {(showFullNav
          ? settingsNavSections
          : [{ label: "引擎", items: compactSettingsNavItems }]
        ).map((section) => (
          <div
            key={section.label}
            className="flex min-w-0 flex-wrap gap-1 lg:flex-col lg:flex-nowrap lg:gap-0.5"
          >
            {showFullNav ? (
              <div className="hidden px-2 pb-1.5 pt-1 font-mono text-2xs font-semibold uppercase tracking-[0.12em] text-subtle-foreground lg:block">
                {section.label}
              </div>
            ) : null}
            {section.items.map((item) => (
              <SettingsNavButton
                key={item.label}
                activePage={activePage}
                item={item}
                onPageChange={onPageChange}
              />
            ))}
          </div>
        ))}
      </div>
    </aside>
  );
}

function RuntimeHint() {
  return (
    <Alert className="border-agent-1/30 bg-agent-1/10 py-3 text-agent-1">
      <Info className="size-4" aria-hidden="true" />
      <AlertTitle className="text-xs font-semibold text-agent-1">
        运行时参数下放到每个 Agent
      </AlertTitle>
      <AlertDescription className="text-2xs leading-relaxed text-agent-1">
        工作目录、权限模式、额外 CLI 参数等按 Agent 单独配置（组织架构 → Agent →
        配置），本页只管理共享后端实例。
      </AlertDescription>
    </Alert>
  );
}

type SettingsPageHeaderProps = {
  description: string;
  title: string;
};

function SettingsPageHeader({ description, title }: SettingsPageHeaderProps) {
  return (
    <div className="flex max-w-3xl flex-col gap-1.5">
      <h1 className="text-2xl font-semibold tracking-normal">{title}</h1>
      <p className="text-sm leading-relaxed text-muted-foreground">
        {description}
      </p>
    </div>
  );
}

type AppearanceSettingsProps = {
  effectiveTheme: AppTheme;
  onThemePreferenceChange: (themePreference: AppThemePreference) => void;
  themePreference: AppThemePreference;
};

const themePreferenceOptions = [
  {
    label: "跟随系统",
    value: "system",
  },
  {
    label: "浅色",
    value: "light",
  },
  {
    label: "深色",
    value: "dark",
  },
] satisfies {
  label: string;
  value: AppThemePreference;
}[];

type ThemePreferenceSelectProps = Omit<
  AppearanceSettingsProps,
  "effectiveTheme"
>;

function ThemePreferenceSelect({
  onThemePreferenceChange,
  themePreference,
}: ThemePreferenceSelectProps) {
  const labelId = React.useId();

  return (
    <div className="flex flex-col gap-2 p-4">
      <div className="flex min-w-0 flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex min-w-0 flex-col gap-0.5">
          <span id={labelId} className="text-sm font-medium">
            主题模式
          </span>
          <p className="text-xs leading-relaxed text-muted-foreground">
            选择界面外观偏好。
          </p>
        </div>
        <div className="w-full sm:w-[220px]">
          <Select
            value={themePreference}
            onValueChange={(value) =>
              onThemePreferenceChange(value as AppThemePreference)
            }
          >
            <SelectTrigger aria-label="主题模式" aria-labelledby={labelId}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {themePreferenceOptions.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>
    </div>
  );
}

function AppearanceSettings({
  effectiveTheme,
  onThemePreferenceChange,
  themePreference,
}: AppearanceSettingsProps) {
  const followsSystem = themePreference === "system";

  return (
    <>
      <SettingsPageHeader
        title="外观"
        description="调整 Agentre 的界面显示方式。主题偏好会保存在当前设备。"
      />
      <section className="overflow-hidden rounded-lg border border-border bg-card">
        <div className="flex flex-wrap items-center gap-3 border-b border-border px-4 py-3">
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <h2 className="text-sm font-semibold">配色模式</h2>
            <p className="text-xs leading-relaxed text-muted-foreground">
              跟随系统或手动固定浅色 / 深色界面。
            </p>
          </div>
          <Badge
            variant="secondary"
            className="rounded-sm px-1.5 py-0 font-mono text-2xs font-medium"
          >
            {followsSystem
              ? "SYSTEM"
              : effectiveTheme === "dark"
                ? "DARK"
                : "LIGHT"}
          </Badge>
        </div>
        <ThemePreferenceSelect
          onThemePreferenceChange={onThemePreferenceChange}
          themePreference={themePreference}
        />
      </section>
    </>
  );
}

function AgentBackendSettings({
  onOpenProxySettings,
}: {
  onOpenProxySettings: () => void;
}) {
  return (
    <>
      <SettingsPageHeader
        title="Agent 后端"
        description="配置 Agent 的执行引擎与对应模型。工作目录 / 权限模式等运行时参数下放到「组织架构 → Agent」单独配置。"
      />
      <div className="flex min-w-0 flex-col gap-3">
        <AgentBackendsPanel onOpenProxySettings={onOpenProxySettings} />
        <RuntimeHint />
      </div>
    </>
  );
}

function LocalProxySettings() {
  return (
    <>
      <SettingsPageHeader
        title="本地 HTTP 代理"
        description="给 App 内置的 LLM Gateway。claudecode / codex 子进程把所有 HTTP 请求打到这里，App 用临时 token 鉴权后转发到真实 LLM 供应商，密钥不出 App。"
      />
      <SettingsProxyPanel />
    </>
  );
}

function LlmProviderSettings() {
  return (
    <>
      <SettingsPageHeader
        title="LLM 供应商"
        description="配置可用模型来源。保存后可拉取该账号下的实际可用模型，模型能力（上下文窗口、是否支持 thinking 等）由 cago agents 内置目录补全。"
      />
      <LlmProvidersPanel />
    </>
  );
}

function SettingsUnderConstruction({ page }: { page: SettingsPageId }) {
  if (
    page === "appearance" ||
    page === "agent-backend" ||
    page === "remote-devices" ||
    page === "keyboard-shortcuts" ||
    page === "llm-providers" ||
    page === "local-proxy" ||
    page === "version-logs" ||
    page === "data-backup"
  ) {
    return null;
  }

  const pageConfig = underConstructionSettingsPages[page];

  return (
    <UnderConstructionPage
      className="px-0 py-0"
      description={pageConfig.description}
      icon={pageConfig.icon}
      title={pageConfig.title}
    />
  );
}

type SettingsPageProps = {
  effectiveTheme: AppTheme;
  onThemePreferenceChange: (themePreference: AppThemePreference) => void;
  themePreference: AppThemePreference;
};

function SettingsPage({
  effectiveTheme,
  onThemePreferenceChange,
  themePreference,
}: SettingsPageProps) {
  const [activePage, setActivePage] =
    React.useState<SettingsPageId>("appearance");

  return (
    <div
      data-slot="settings-page"
      className="flex min-h-0 min-w-0 flex-1 flex-col lg:flex-row"
    >
      <SettingsNav activePage={activePage} onPageChange={setActivePage} />
      <main className="min-w-0 flex-1 overflow-auto bg-background">
        <div className="flex min-h-full w-full min-w-0 max-w-[1180px] flex-col gap-6 px-4 py-5 sm:px-6 lg:gap-8 lg:px-10 lg:py-8">
          {activePage === "remote-devices" ? (
            <RemoteDevicesPanel />
          ) : activePage === "appearance" ? (
            <AppearanceSettings
              effectiveTheme={effectiveTheme}
              onThemePreferenceChange={onThemePreferenceChange}
              themePreference={themePreference}
            />
          ) : activePage === "agent-backend" ? (
            <AgentBackendSettings
              onOpenProxySettings={() => setActivePage("local-proxy")}
            />
          ) : activePage === "llm-providers" ? (
            <LlmProviderSettings />
          ) : activePage === "local-proxy" ? (
            <LocalProxySettings />
          ) : activePage === "keyboard-shortcuts" ? (
            <KeyboardShortcutsPanel />
          ) : activePage === "data-backup" ? (
            <DataBackupPanel />
          ) : activePage === "version-logs" ? (
            <UpdateSection />
          ) : (
            <SettingsUnderConstruction page={activePage} />
          )}
        </div>
      </main>
    </div>
  );
}

export { SettingsPage };
