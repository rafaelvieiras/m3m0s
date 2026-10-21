import type { APIRoute, GetStaticPaths } from "astro";
import { renderOg } from "../../../lib/og";
import { sortedGuide, type GuideEntry } from "../../../lib/guide";

export const getStaticPaths = (async () => {
  const entries = await sortedGuide();
  return entries.map((entry) => ({ params: { slug: entry.id }, props: { entry } }));
}) satisfies GetStaticPaths;

export const GET: APIRoute = async ({ props }) => {
  const { entry } = props as { entry: GuideEntry };
  const png = await renderOg({
    title: entry.data.title,
    description: entry.data.description,
    kicker: "Guide",
  });
  return new Response(new Uint8Array(png), { headers: { "Content-Type": "image/png" } });
};
