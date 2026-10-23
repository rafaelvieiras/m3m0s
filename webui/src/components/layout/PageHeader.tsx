// Page title with an optional subtitle and a collapsible plain-language help
// blurb ("About this page"), so every screen says what it is for.
import type { ReactNode } from "react";
import { ChevronRight } from "lucide-react";
import { useT, type TKey } from "@/i18n";
import { HelpTip, type GlossaryTerm } from "@/components/help/HelpTip";

export type HelpPage =
  | "home"
  | "search"
  | "add"
  | "recent"
  | "stats"
  | "insights"
  | "jobs"
  | "embedding"
  | "preview"
  | "connect";

interface PageHeaderProps {
  title: string;
  subtitle?: ReactNode;
  help?: HelpPage;
  term?: GlossaryTerm;
  actions?: ReactNode;
}

export function PageHeader({ title, subtitle, help, term, actions }: PageHeaderProps) {
  const { t } = useT();
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0">
          <h1 className="flex items-center gap-1 text-lg font-semibold">
            {title}
            {term ? <HelpTip term={term} /> : null}
          </h1>
          {subtitle ? <p className="text-sm text-muted-foreground">{subtitle}</p> : null}
        </div>
        {actions}
      </div>
      {help ? (
        <details className="group text-sm text-muted-foreground">
          <summary className="inline-flex cursor-pointer list-none items-center gap-1 rounded text-xs outline-none hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 [&::-webkit-details-marker]:hidden">
            <ChevronRight className="size-3.5 transition-transform group-open:rotate-90" aria-hidden />
            {t("help.about")}
          </summary>
          <p className="mt-1 max-w-2xl rounded-md bg-muted/50 p-2.5">{t(`help.page.${help}` as TKey)}</p>
        </details>
      ) : null}
    </div>
  );
}
