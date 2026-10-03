// GET /preview/injection (D29) — preview of what each context injection
// mechanism would produce today, for a real domain, before deciding to
// implement new hooks/integrations for other tools.
import { createRoute, lazyRouteComponent } from "@tanstack/react-router";
import { z } from "zod";
import { previewInjectionModeSchema } from "@/lib/schemas";
import { Route as AuthedRoute } from "./_authed";

export const previewSearchSchema = z.object({
  domain: z.string().catch("").default(""),
  mode: previewInjectionModeSchema.catch("hook").default("hook"),
});

export const Route = createRoute({
  getParentRoute: () => AuthedRoute,
  path: "/preview",
  validateSearch: previewSearchSchema,
  component: lazyRouteComponent(() => import("@/pages/PreviewPage")),
});
