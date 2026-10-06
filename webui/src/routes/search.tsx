// Semantic search route (POST /search) — filters on the 4 axes + query live
// in the URL itself (search params), not in a loose `useState`, so the
// screen is a truly shareable/back-navigable link (TanStack Router).
import { createRoute, lazyRouteComponent } from "@tanstack/react-router";
import { z } from "zod";
import { Route as AuthedRoute } from "./_authed";

export const searchSearchSchema = z.object({
  q: z.string().catch("").default(""),
  domain: z.string().catch("").default(""),
  domainCategory: z.string().catch("").default(""),
  source: z.string().catch("").default(""),
  agent: z.string().catch("").default(""),
  model: z.string().catch("").default(""),
  project: z.string().catch("").default(""),
  tags: z.string().catch("").default(""),
});

export const Route = createRoute({
  getParentRoute: () => AuthedRoute,
  path: "/search",
  validateSearch: searchSearchSchema,
  component: lazyRouteComponent(() => import("@/pages/SearchPage")),
});
