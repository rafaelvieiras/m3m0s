import type { APIRoute } from "astro";
import { renderOg } from "../../lib/og";

export const GET: APIRoute = async () => {
  const png = await renderOg({
    title: "One long-term memory for every agent you use.",
    description: "Self-hosted. One Go binary, one SQLite file. Shared across Claude Code, pi, opencode, Hermes and any MCP client.",
  });
  return new Response(new Uint8Array(png), { headers: { "Content-Type": "image/png" } });
};
