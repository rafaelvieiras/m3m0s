import { defineConfig } from "astro/config";
import sitemap from "@astrojs/sitemap";

export default defineConfig({
  site: "https://m3m0s.rafaelvieiras.com",
  i18n: { defaultLocale: "en", locales: ["en", "pt", "es"], routing: { prefixDefaultLocale: false } },
  integrations: [
    sitemap({
      i18n: { defaultLocale: "en", locales: { en: "en-US", pt: "pt-BR", es: "es" } },
    }),
  ],
  markdown: {
    shikiConfig: { themes: { light: "github-light", dark: "github-dark-dimmed" } },
  },
});
