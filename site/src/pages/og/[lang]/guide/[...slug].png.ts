import type { APIRoute, GetStaticPaths } from "astro";
import { renderOg } from "../../../../lib/og";
import { sortedGuide, slugOf, type GuideEntry } from "../../../../lib/guide";
import { LOCALES, t, type Lang } from "../../../../i18n";

export const getStaticPaths = (async () => {
  const out = [];
  for (const lang of LOCALES) {
    for (const entry of await sortedGuide(lang)) out.push({ params: { lang, slug: slugOf(entry) }, props: { entry } });
  }
  return out;
}) satisfies GetStaticPaths;

export const GET: APIRoute = async ({ params, props }) => {
  const { entry } = props as { entry: GuideEntry };
  const d = t(params.lang as Lang);
  const png = await renderOg({
    title: entry.data.title,
    description: entry.data.description,
    kicker: d.guide.kicker,
    tagline: d.site.tagline,
  });
  return new Response(new Uint8Array(png), { headers: { "Content-Type": "image/png" } });
};
