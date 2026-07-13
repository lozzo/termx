import { Languages } from "lucide-react";
import { useTranslation } from "react-i18next";
import { languages, type AppLanguage } from "@/i18n";
import { cn } from "@/lib/utils";

// LanguageSwitcher 只管理浏览器本地表现偏好；切换事件由 i18next 广播，不向 Control Plane 写入账号状态。
export function LanguageSwitcher({ compact = false, inverse = false }: { compact?: boolean; inverse?: boolean }) {
  const { i18n, t } = useTranslation();
  return (
    <label className={cn("relative flex h-11 min-w-28 items-center border border-line bg-panel text-[10px] text-muted-foreground", compact && "min-w-0", inverse && "border-white/25 bg-transparent text-white/70")}>
      <Languages className="pointer-events-none ml-3 size-3.5 shrink-0" />
      <span className="sr-only">{t("common.language")}</span>
      <select
        className={cn("h-full min-w-0 flex-1 cursor-pointer appearance-none bg-transparent px-2 pr-5 font-semibold outline-none", compact && "w-14", inverse && "text-white")}
        aria-label={t("common.language")}
        value={i18n.resolvedLanguage ?? "en"}
        onChange={(event) => void i18n.changeLanguage(event.target.value as AppLanguage)}
      >
        {languages.map((language) => <option className="bg-panel text-foreground" key={language.id} value={language.id}>{compact ? language.short : language.label}</option>)}
      </select>
    </label>
  );
}
