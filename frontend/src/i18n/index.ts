import i18n from "i18next";
import { initReactI18next } from "react-i18next";

import enCommon from "./locales/en/common.json";
import zhCommon from "./locales/zh-CN/common.json";

export const LANGUAGE_STORAGE_KEY = "agentre.language";

export const supportedLanguages = ["zh-CN", "en"] as const;

export type SupportedLanguage = (typeof supportedLanguages)[number];

type LanguageStorage = Pick<Storage, "getItem"> &
  Partial<Pick<Storage, "setItem">>;

type DetectInitialLanguageOptions = {
  navigatorLanguage?: string | null;
  navigatorLanguages?: readonly string[] | null;
  storage?: LanguageStorage | null;
};

const resources = {
  "zh-CN": { common: zhCommon },
  en: { common: enCommon },
};

function normalizeStoredLanguage(value: string | null | undefined) {
  if (!value) return null;

  const normalized = value.trim().toLowerCase();
  if (normalized === "zh-cn") return "zh-CN";
  if (normalized === "en") return "en";

  return null;
}

function normalizeNavigatorLanguage(value: string | null | undefined) {
  if (!value) return null;

  const normalized = value.trim().toLowerCase();
  if (
    normalized === "zh" ||
    normalized === "zh-cn" ||
    normalized.startsWith("zh-hans")
  ) {
    return "zh-CN";
  }
  if (normalized === "en" || normalized.startsWith("en-")) return "en";

  return null;
}

function languageFromNavigator({
  navigatorLanguage,
  navigatorLanguages,
}: Pick<
  DetectInitialLanguageOptions,
  "navigatorLanguage" | "navigatorLanguages"
>): SupportedLanguage {
  const candidates = [
    ...(navigatorLanguages ?? []),
    ...(navigatorLanguage ? [navigatorLanguage] : []),
  ];

  for (const candidate of candidates) {
    const supportedLanguage = normalizeNavigatorLanguage(candidate);
    if (supportedLanguage) return supportedLanguage;
  }

  return "en";
}

function readStoredLanguage(storage: LanguageStorage | null) {
  if (!storage) return null;

  try {
    return normalizeStoredLanguage(storage.getItem(LANGUAGE_STORAGE_KEY));
  } catch {
    return null;
  }
}

function writeStoredLanguage(
  storage: LanguageStorage | null,
  language: SupportedLanguage,
) {
  if (!storage?.setItem) return;

  try {
    storage.setItem(LANGUAGE_STORAGE_KEY, language);
  } catch {
    return;
  }
}

function getBrowserStorage() {
  if (typeof window === "undefined") return null;

  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

function getNavigatorLanguage() {
  if (typeof navigator === "undefined") return null;

  return navigator.language;
}

function getNavigatorLanguages() {
  if (typeof navigator === "undefined") return null;

  return navigator.languages.length > 0 ? navigator.languages : null;
}

export function detectInitialLanguage({
  navigatorLanguage,
  navigatorLanguages,
  storage,
}: DetectInitialLanguageOptions = {}): SupportedLanguage {
  const languageStorage = storage ?? null;
  const storedLanguage = readStoredLanguage(languageStorage);
  if (storedLanguage) return storedLanguage;

  const detectedLanguage = languageFromNavigator({
    navigatorLanguage,
    navigatorLanguages,
  });
  writeStoredLanguage(languageStorage, detectedLanguage);
  return detectedLanguage;
}

i18n.use(initReactI18next).init({
  defaultNS: "common",
  fallbackLng: "en",
  interpolation: {
    escapeValue: false,
  },
  lng: detectInitialLanguage({
    navigatorLanguage: getNavigatorLanguage(),
    navigatorLanguages: getNavigatorLanguages(),
    storage: getBrowserStorage(),
  }),
  resources,
  react: {
    useSuspense: false,
  },
});

export default i18n;
