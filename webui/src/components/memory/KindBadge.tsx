// Colored badge for a memory's kind (decision, rule...). Unclassified memories
// ("other") get the neutral style. The title explains the kind and who tagged it.
import { Badge } from "@/components/ui/badge";
import { useT } from "@/i18n";
import { KIND_BADGE_CLASS, isKind, kindDescKey, kindLabel, kindSourceLabel } from "@/lib/kinds";
import { cn } from "@/lib/utils";

export function KindBadge({ kind, kindSource, className }: { kind?: string; kindSource?: string; className?: string }) {
  const { t } = useT();
  const value = isKind(kind) ? kind : "other";
  const by = kindSource ? t("memory.detail.kindSetBy", { source: kindSourceLabel(kindSource) }) : t("memory.detail.neverClassified");
  return (
    <Badge variant="outline" className={cn(KIND_BADGE_CLASS[value], className)} title={`${t(kindDescKey(value))} ${by}`}>
      {kindLabel(value)}
    </Badge>
  );
}
