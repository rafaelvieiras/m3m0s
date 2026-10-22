import type { APIRoute } from "astro";
import { sortedGuide, slugOf } from "../lib/guide";
import { LOCALES, LANG_META, localePath, t } from "../i18n";

export const GET: APIRoute = async ({ site }) => {
  const abs = (p: string) => new URL(p, site).href;
  const en = t("en");
  let out = `# m3m0s\n\n> ${en.schema.appDescription}\n\n`;
  out += "m3m0s captures what coding agents learn, stores it as short facts in SQLite with embeddings, and recalls the relevant ones on each prompt. It ships a CLI, an HTTP API, an MCP server and a WebUI. MIT licensed.\n\n";
  for (const lang of LOCALES) {
    const entries = await sortedGuide(lang);
    out += `## Guide (${LANG_META[lang].label})\n\n`;
    for (const e of entries) {
      const slug = slugOf(e);
      out += `- [${e.data.title}](${abs(localePath(lang, slug === "index" ? "/guide/index.md" : `/guide/${slug}.md`))}): ${e.data.description}\n`;
    }
    out += "\n";
  }
  out += `## Optional\n\n- [Full guide in one file (English)](${abs("/llms-full.txt")})\n- [Source code](https://github.com/rafaelvieiras/m3m0s)\n- [Releases](https://github.com/rafaelvieiras/m3m0s/releases)\n- [Security policy](https://github.com/rafaelvieiras/m3m0s/blob/main/SECURITY.md)\n`;
  return new Response(out, { headers: { "Content-Type": "text/plain; charset=utf-8" } });
};
