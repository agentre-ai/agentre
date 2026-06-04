import * as React from "react";
import { useTranslation } from "react-i18next";
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
import { LANGUAGE_STORAGE_KEY, type SupportedLanguage } from "@/i18n";

import type { AppTheme, AppThemePreference } from "./chrome";
import { AgentBackendsPanel } from "./agent-backends";
import { DataBackupPanel } from "./data-backup";
import { RemoteDevicesPanel } from "./remote-devices/remote-devices-panel";
import { LlmProvidersPanel } from "./llm-providers";
import { SettingsProxyPanel } from "./settings-proxy";
import { NotificationsPanel } from "./notifications-panel";
import { KeyboardShortcutsPanel } from "./shortcuts";
import { UnderConstructionPage } from "./under-construction-page";
import { UpdateSection } from "./update-section";

type SettingsNavSection = {
  labelKey: string;
  items: {
    id?: SettingsPageId;
    icon: LucideIcon;
    labelKey: string;
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
    labelKey: "settings.nav.general",
    items: [
      { icon: SunMoon, id: "appearance", labelKey: "settings.nav.appearance" },
      {
        icon: Bell,
        id: "notifications",
        labelKey: "settings.nav.notifications",
      },
      {
        icon: Keyboard,
        id: "keyboard-shortcuts",
        labelKey: "settings.nav.keyboardShortcuts",
      },
      {
        icon: Database,
        id: "data-backup",
        labelKey: "settings.nav.dataBackup",
      },
    ],
  },
  {
    labelKey: "settings.nav.engine",
    items: [
      {
        icon: Sparkles,
        id: "llm-providers",
        labelKey: "settings.nav.llmProvider",
      },
      { icon: Cpu, id: "agent-backend", labelKey: "settings.nav.agentBackend" },
    ],
  },
  {
    labelKey: "settings.nav.integrations",
    items: [
      { icon: Network, id: "local-proxy", labelKey: "settings.nav.localProxy" },
      { icon: Server, id: "mcp-servers", labelKey: "settings.nav.mcpServers" },
      {
        icon: Wrench,
        id: "skills-tools",
        labelKey: "settings.nav.skillsTools",
      },
      {
        icon: Cable,
        id: "remote-devices",
        labelKey: "settings.nav.remoteDevices",
      },
    ],
  },
  {
    labelKey: "settings.nav.about",
    items: [
      { icon: Info, id: "version-logs", labelKey: "settings.nav.versionLogs" },
    ],
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
    | "notifications"
  >,
  {
    descriptionKey: string;
    icon: LucideIcon;
    titleKey: string;
  }
> = {
  "mcp-servers": {
    titleKey: "settings.underConstruction.mcpServers.title",
    descriptionKey: "settings.underConstruction.mcpServers.description",
    icon: Server,
  },
  "skills-tools": {
    titleKey: "settings.underConstruction.skillsTools.title",
    descriptionKey: "settings.underConstruction.skillsTools.description",
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
    labelKey: string;
  };
  onPageChange: (page: SettingsPageId) => void;
};

function SettingsNavButton({
  activePage,
  item,
  onPageChange,
}: SettingsNavButtonProps) {
  const { t } = useTranslation();
  const Icon = item.icon;
  const active = item.id === activePage;
  const pageId = item.id;
  const label = t(item.labelKey);

  return (
    <Button
      key={item.labelKey}
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
      {label}
    </Button>
  );
}

function SettingsNav({ activePage, onPageChange }: SettingsNavProps) {
  const { t } = useTranslation();
  const showFullNav = useMediaQuery("(min-width: 1024px)");

  return (
    <aside
      aria-label={t("settings.nav.settings")}
      className="flex w-full shrink-0 flex-col gap-2 border-b border-border bg-sidebar px-3 py-3 lg:w-[220px] lg:gap-[18px] lg:border-b-0 lg:border-r lg:py-4"
    >
      <div className="px-1.5 text-sm font-semibold lg:pb-2">
        {t("settings.nav.settings")}
      </div>
      <div className="flex flex-wrap gap-1.5 pb-1 lg:flex-col lg:flex-nowrap lg:gap-[18px] lg:p-0">
        {(showFullNav
          ? settingsNavSections
          : [
              {
                labelKey: "settings.nav.engine",
                items: compactSettingsNavItems,
              },
            ]
        ).map((section) => (
          <div
            key={section.labelKey}
            className="flex min-w-0 flex-wrap gap-1 lg:flex-col lg:flex-nowrap lg:gap-0.5"
          >
            {showFullNav ? (
              <div className="hidden px-2 pb-1.5 pt-1 font-mono text-2xs font-semibold uppercase tracking-[0.12em] text-subtle-foreground lg:block">
                {t(section.labelKey)}
              </div>
            ) : null}
            {section.items.map((item) => (
              <SettingsNavButton
                key={item.labelKey}
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
  const { t } = useTranslation();

  return (
    <Alert className="border-agent-1/30 bg-agent-1/10 py-3 text-agent-1">
      <Info className="size-4" aria-hidden="true" />
      <AlertTitle className="text-xs font-semibold text-agent-1">
        {t("settings.agentBackend.runtimeHint.title")}
      </AlertTitle>
      <AlertDescription className="text-2xs leading-relaxed text-agent-1">
        {t("settings.agentBackend.runtimeHint.description")}
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
    labelKey: "theme.system",
    value: "system",
  },
  {
    labelKey: "theme.light",
    value: "light",
  },
  {
    labelKey: "theme.dark",
    value: "dark",
  },
] satisfies {
  labelKey: string;
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
  const { t } = useTranslation();
  const labelId = React.useId();

  return (
    <div className="flex flex-col gap-2 p-4">
      <div className="flex min-w-0 flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex min-w-0 flex-col gap-0.5">
          <span id={labelId} className="text-sm font-medium">
            {t("settings.appearance.themeMode.label")}
          </span>
          <p className="text-xs leading-relaxed text-muted-foreground">
            {t("settings.appearance.themeMode.description")}
          </p>
        </div>
        <div className="w-full sm:w-[220px]">
          <Select
            value={themePreference}
            onValueChange={(value) =>
              onThemePreferenceChange(value as AppThemePreference)
            }
          >
            <SelectTrigger
              aria-label={t("settings.appearance.themeMode.label")}
              aria-labelledby={labelId}
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {themePreferenceOptions.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {t(option.labelKey)}
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
  const { i18n, t } = useTranslation();
  const followsSystem = themePreference === "system";
  const language =
    i18n.resolvedLanguage === "zh-CN" || i18n.resolvedLanguage === "en"
      ? i18n.resolvedLanguage
      : "en";

  function handleLanguageChange(next: string) {
    const supportedLanguage = next as SupportedLanguage;
    void i18n.changeLanguage(supportedLanguage);

    try {
      localStorage.setItem(LANGUAGE_STORAGE_KEY, supportedLanguage);
    } catch {
      // Embedded previews may block localStorage.
    }
  }

  return (
    <>
      <SettingsPageHeader
        title={t("settings.appearance.title")}
        description={t("settings.appearance.description")}
      />
      <section className="overflow-hidden rounded-lg border border-border bg-card">
        <div className="flex flex-wrap items-center gap-3 border-b border-border px-4 py-3">
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <h2 className="text-sm font-semibold">
              {t("settings.appearance.colorMode.title")}
            </h2>
            <p className="text-xs leading-relaxed text-muted-foreground">
              {t("settings.appearance.colorMode.description")}
            </p>
          </div>
          <Badge
            variant="secondary"
            className="rounded-sm px-1.5 py-0 font-mono text-2xs font-medium"
          >
            {followsSystem
              ? t("theme.system")
              : effectiveTheme === "dark"
                ? t("theme.dark")
                : t("theme.light")}
          </Badge>
        </div>
        <ThemePreferenceSelect
          onThemePreferenceChange={onThemePreferenceChange}
          themePreference={themePreference}
        />
        <div className="flex flex-col gap-2 border-t border-border p-4">
          <div className="flex min-w-0 flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex min-w-0 flex-col gap-0.5">
              <span className="text-sm font-medium">{t("language.label")}</span>
              <p className="text-xs leading-relaxed text-muted-foreground">
                {t("language.description")}
              </p>
            </div>
            <div className="w-full sm:w-[220px]">
              <Select value={language} onValueChange={handleLanguageChange}>
                <SelectTrigger aria-label={t("language.label")}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="zh-CN">{t("language.zh-CN")}</SelectItem>
                  <SelectItem value="en">{t("language.en")}</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
        </div>
      </section>
    </>
  );
}

function AgentBackendSettings({
  onOpenProxySettings,
}: {
  onOpenProxySettings: () => void;
}) {
  const { t } = useTranslation();

  return (
    <>
      <SettingsPageHeader
        title={t("settings.agentBackend.title")}
        description={t("settings.agentBackend.description")}
      />
      <div className="flex min-w-0 flex-col gap-3">
        <AgentBackendsPanel onOpenProxySettings={onOpenProxySettings} />
        <RuntimeHint />
      </div>
    </>
  );
}

function LocalProxySettings() {
  const { t } = useTranslation();

  return (
    <>
      <SettingsPageHeader
        title={t("settings.localProxy.title")}
        description={t("settings.localProxy.description")}
      />
      <SettingsProxyPanel />
    </>
  );
}

function LlmProviderSettings() {
  const { t } = useTranslation();

  return (
    <>
      <SettingsPageHeader
        title={t("settings.llmProvider.title")}
        description={t("settings.llmProvider.description")}
      />
      <LlmProvidersPanel />
    </>
  );
}

function SettingsUnderConstruction({ page }: { page: SettingsPageId }) {
  const { t } = useTranslation();

  if (
    page === "appearance" ||
    page === "agent-backend" ||
    page === "remote-devices" ||
    page === "keyboard-shortcuts" ||
    page === "llm-providers" ||
    page === "local-proxy" ||
    page === "version-logs" ||
    page === "data-backup" ||
    page === "notifications"
  ) {
    return null;
  }

  const pageConfig = underConstructionSettingsPages[page];

  return (
    <UnderConstructionPage
      className="px-0 py-0"
      description={t(pageConfig.descriptionKey)}
      icon={pageConfig.icon}
      title={t(pageConfig.titleKey)}
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
          ) : activePage === "notifications" ? (
            <NotificationsPanel />
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
