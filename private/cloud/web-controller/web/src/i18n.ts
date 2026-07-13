import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import en from "./locales/en.json";
import zhCN from "./locales/zh-CN.json";
import ru from "./locales/ru.json";

// AppLanguage 是 Web Controller 表现层允许持久化的语言标识；它不进入账号或 Control Plane 领域状态。
export type AppLanguage = "en" | "zh-CN" | "ru";
// languages 是语言选择器的唯一选项来源，id 必须与下方 i18next resources 的键保持一致。
export const languages: { id: AppLanguage; label: string; short: string }[] = [
  { id: "en", label: "English", short: "EN" },
  { id: "zh-CN", label: "中文", short: "中文" },
  { id: "ru", label: "Русский", short: "RU" }
];

function normalizeLanguage(value?: string | null): AppLanguage {
  const language = value?.toLowerCase() ?? "";
  if (language.startsWith("zh")) return "zh-CN";
  if (language.startsWith("ru")) return "ru";
  return "en";
}

const initialLanguage = normalizeLanguage(localStorage.getItem("termx-language") ?? navigator.language);

void i18n.use(initReactI18next).init({
  resources: { en: { translation: en }, "zh-CN": { translation: zhCN }, ru: { translation: ru } },
  lng: initialLanguage,
  fallbackLng: "en",
  interpolation: { escapeValue: false },
  returnObjects: true
});

document.documentElement.lang = initialLanguage;
i18n.on("languageChanged", (language) => {
  const normalized = normalizeLanguage(language);
  localStorage.setItem("termx-language", normalized);
  document.documentElement.lang = normalized;
});

// intlLocale 把 i18next 的当前语言收敛为 Intl 可消费的 locale；未知语言按英文回退。
export function intlLocale(language: string): string {
  return normalizeLanguage(language);
}

export default i18n;
