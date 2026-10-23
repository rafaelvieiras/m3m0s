// A small "?" button that opens a one-sentence, plain-language explanation of
// a term. Backed by the glossary in i18n/locales/*/help.ts; it is a popover
// (not a hover tooltip) so it also works by tap and keyboard.
import { CircleHelp } from "lucide-react";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { useT, type TKey } from "@/i18n";
import { cn } from "@/lib/utils";

export type GlossaryTerm =
  | "memory"
  | "kind"
  | "domain"
  | "project"
  | "embedding"
  | "jobs"
  | "insights"
  | "preview"
  | "forgetting"
  | "apiKey";

export function HelpTip({ term, className }: { term: GlossaryTerm; className?: string }) {
  const { t } = useT();
  const name = t(`help.term.${term}.name` as TKey);
  return (
    <Popover>
      <PopoverTrigger
        type="button"
        aria-label={t("help.term", { term: name })}
        className={cn(
          "inline-flex size-5 shrink-0 items-center justify-center rounded-full text-muted-foreground outline-none hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50",
          className,
        )}
      >
        <CircleHelp className="size-3.5" aria-hidden />
      </PopoverTrigger>
      <PopoverContent>
        <p className="mb-1 font-medium">{name}</p>
        <p className="text-muted-foreground">{t(`help.term.${term}.body` as TKey)}</p>
      </PopoverContent>
    </Popover>
  );
}
