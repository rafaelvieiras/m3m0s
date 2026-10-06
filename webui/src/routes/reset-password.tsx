// Public route (no auth guard): the single-use link printed by
// `m3m0s user reset|add` lands here with ?token=...
import { createRoute, lazyRouteComponent } from "@tanstack/react-router";
import { z } from "zod";
import { Route as RootRoute } from "./__root";

export const Route = createRoute({
  getParentRoute: () => RootRoute,
  path: "/reset-password",
  validateSearch: z.object({ token: z.string().catch("").default("") }),
  component: lazyRouteComponent(() => import("@/pages/ResetPasswordPage")),
});
