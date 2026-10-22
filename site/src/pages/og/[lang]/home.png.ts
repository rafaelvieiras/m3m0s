import type { APIRoute, GetStaticPaths } from "astro";
import { renderOg } from "../../../lib/og";
import { LOCALES, t, type Lang } from "../../../i18n";

export const getStaticPaths = (() => LOCALES.map((lang) => ({ params: { lang } }))) satisfies GetStaticPaths;

export const GET: APIRoute = async ({ params }) => {
  const d = t(params.lang as Lang);
  const png = await renderOg({ title: d.site.ogTitle, description: d.site.ogDescription, tagline: d.site.tagline });
  return new Response(new Uint8Array(png), { headers: { "Content-Type": "image/png" } });
};
