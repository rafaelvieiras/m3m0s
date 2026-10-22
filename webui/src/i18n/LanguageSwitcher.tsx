import { Languages } from "lucide-react";
import { LOCALES, LOCALE_LABELS, setLocale, useT, type Locale } from "@/i18n";

/** Native <select> keeps this accessible and dependency-free. */
export function LanguageSwitcher({ className }: { className?: string }) {
  const { t, locale } = useT();
  return (
    <label className={`inline-flex items-center gap-1.5 text-sm text-muted-foreground ${className ?? ""}`}>
      <Languages className="size-4" aria-hidden="true" />
      <span className="sr-only">{t("common.language")}</span>
      <select
        value={locale}
        onChange={(e) => setLocale(e.target.value as Locale)}
        className="rounded-md border bg-background px-1.5 py-1 text-sm text-foreground"
      >
        {LOCALES.map((l) => (
          <option key={l} value={l}>
            {LOCALE_LABELS[l]}
          </option>
        ))}
      </select>
    </label>
  );
}
