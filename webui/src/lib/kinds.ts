// The closed set of memory "kinds" (internal/factkind) with their friendly
// labels and badge colors. The server speaks the raw names; people see the
// translated label and a one-sentence explanation.
import { translate, type TKey } from "@/i18n";

export const KINDS = ["decision", "rule", "preference", "gotcha", "procedure", "state", "noise", "other"] as const;
export type Kind = (typeof KINDS)[number];

export function isKind(v: string | undefined | null): v is Kind {
  return !!v && (KINDS as readonly string[]).includes(v);
}

export function kindLabelKey(kind: Kind): TKey {
  return `kinds.${kind}.label`;
}

export function kindDescKey(kind: Kind): TKey {
  return `kinds.${kind}.desc`;
}

/** Friendly label for a raw kind; unknown values are returned as they came. */
export function kindLabel(kind: string): string {
  return isKind(kind) ? translate(kindLabelKey(kind)) : kind;
}

const KIND_SOURCES = ["llm", "heuristic", "manual", "none"] as const;

/** Friendly label for who tagged the kind (llm, heuristic, manual, none). */
export function kindSourceLabel(source: string): string {
  return (KIND_SOURCES as readonly string[]).includes(source)
    ? translate(`kinds.source.${source as (typeof KIND_SOURCES)[number]}`)
    : source;
}

// One soft color per kind, readable in light and dark mode.
export const KIND_BADGE_CLASS: Record<Kind, string> = {
  decision: "border-transparent bg-blue-500/15 text-blue-700 dark:text-blue-300",
  rule: "border-transparent bg-violet-500/15 text-violet-700 dark:text-violet-300",
  preference: "border-transparent bg-pink-500/15 text-pink-700 dark:text-pink-300",
  gotcha: "border-transparent bg-amber-500/20 text-amber-800 dark:text-amber-300",
  procedure: "border-transparent bg-emerald-500/15 text-emerald-700 dark:text-emerald-300",
  state: "border-transparent bg-sky-500/15 text-sky-700 dark:text-sky-300",
  noise: "border-transparent bg-muted text-muted-foreground",
  other: "border-transparent bg-muted text-muted-foreground",
};
