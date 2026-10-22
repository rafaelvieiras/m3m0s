import type { APIRoute, GetStaticPaths } from "astro";
import { sortedGuide, slugOf, guideHref, type GuideEntry } from "../../../lib/guide";
import { LOCALES, DEFAULT_LANG, type Lang } from "../../../i18n";

export const getStaticPaths = (async () => {
  const out = [];
  for (const lang of LOCALES.filter((l) => l !== DEFAULT_LANG)) {
    for (const entry of await sortedGuide(lang)) out.push({ params: { lang, slug: slugOf(entry) }, props: { entry } });
  }
  return out;
}) satisfies GetStaticPaths;

export const GET: APIRoute = ({ params, props, site }) => {
  const { entry } = props as { entry: GuideEntry };
  const lang = params.lang as Lang;
  const url = new URL(guideHref(lang, entry), site).href;
  const text = entry.body ?? "";
  const body = `# ${entry.data.title}\n\n> ${entry.data.description}\n\nSource: ${url}\n\n${text.trim()}\n`;
  return new Response(body, { headers: { "Content-Type": "text/markdown; charset=utf-8" } });
};
