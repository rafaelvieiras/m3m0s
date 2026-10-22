import { getCollection, type CollectionEntry } from "astro:content";
import { localePath, type Lang } from "../i18n";

export const SECTIONS = ["Start", "Deploy", "Connect your tools", "Reference"] as const;

export type GuideEntry = CollectionEntry<"guide">;

/** Content ids look like "pt/quickstart"; the slug is the part after the locale. */
export function slugOf(entry: GuideEntry): string {
  return entry.id.split("/").slice(1).join("/") || "index";
}

export async function sortedGuide(lang: Lang): Promise<GuideEntry[]> {
  const all = await getCollection("guide", (e) => e.id === lang || e.id.startsWith(`${lang}/`));
  return all.sort((a, b) => a.data.order - b.data.order);
}

export function guideHref(lang: Lang, entry: GuideEntry): string {
  const slug = slugOf(entry);
  return localePath(lang, slug === "index" ? "/guide/" : `/guide/${slug}/`);
}

export function ogPathFor(lang: Lang, entry: GuideEntry): string {
  return `/og/${lang}/guide/${slugOf(entry)}.png`;
}
