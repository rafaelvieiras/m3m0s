import { defineConfig } from "astro/config";
import sitemap from "@astrojs/sitemap";

export default defineConfig({
  site: "https://m3m0s.rafaelvieiras.com",
  integrations: [sitemap()],
  markdown: {
    shikiConfig: { themes: { light: "github-light", dark: "github-dark-dimmed" } },
  },
});
