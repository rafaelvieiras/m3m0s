import type { APIRoute } from "astro";
import { sortedGuide, guideHref } from "../lib/guide";
import { t } from "../i18n";

export const GET: APIRoute = async ({ site }) => {
  const entries = await sortedGuide("en");
  let out = `# m3m0s\n\n> ${t("en").schema.appDescription}\n\n`;
  for (const e of entries) {
    out += `\n---\n\n# ${e.data.title}\n\n> ${e.data.description}\n\nSource: ${new URL(guideHref("en", e), site).href}\n\n${(e.body ?? "").trim()}\n`;
  }
  return new Response(out, { headers: { "Content-Type": "text/plain; charset=utf-8" } });
};
