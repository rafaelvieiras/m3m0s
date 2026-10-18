import { getCollection, type CollectionEntry } from "astro:content";

export const SECTIONS = ["Start", "Deploy", "Connect your tools", "Reference"] as const;

export type GuideEntry = CollectionEntry<"guide">;

export async function sortedGuide(): Promise<GuideEntry[]> {
  const all = await getCollection("guide");
  return all.sort((a, b) => a.data.order - b.data.order);
}

export function guideHref(entry: GuideEntry): string {
  return entry.id === "index" ? "/guide/" : `/guide/${entry.id}/`;
}

export function ogPathFor(entry: GuideEntry): string {
  return `/og/guide/${entry.id}.png`;
}
