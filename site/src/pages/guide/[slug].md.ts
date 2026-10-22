import type { APIRoute, GetStaticPaths } from "astro";
import { sortedGuide, slugOf, guideHref, type GuideEntry } from "../../lib/guide";

export const getStaticPaths = (async () => {
  const entries = await sortedGuide("en");
  return entries.map((entry) => ({ params: { slug: slugOf(entry) }, props: { entry } }));
}) satisfies GetStaticPaths;

export const GET: APIRoute = ({ props, site }) => {
  const { entry } = props as { entry: GuideEntry };
  const url = new URL(guideHref("en", entry), site).href;
  const body = `# ${entry.data.title}\n\n> ${entry.data.description}\n\nSource: ${url}\n\n${(entry.body ?? "").trim()}\n`;
  return new Response(body, { headers: { "Content-Type": "text/markdown; charset=utf-8" } });
};
