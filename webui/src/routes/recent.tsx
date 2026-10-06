// GET /recent — same exact filters as search, without query or tags
// (docs/api-contract.md: recent does not support a tags filter).
import { createRoute, lazyRouteComponent } from "@tanstack/react-router";
import { z } from "zod";
import { Route as AuthedRoute } from "./_authed";

export const recentSearchSchema = z.object({
  domain: z.string().catch("").default(""),
  domainCategory: z.string().catch("").default(""),
  source: z.string().catch("").default(""),
  agent: z.string().catch("").default(""),
  model: z.string().catch("").default(""),
  project: z.string().catch("").default(""),
});

export const Route = createRoute({
  getParentRoute: () => AuthedRoute,
  path: "/recent",
  validateSearch: recentSearchSchema,
  component: lazyRouteComponent(() => import("@/pages/RecentPage")),
});
