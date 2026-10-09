import { createRoute, lazyRouteComponent } from "@tanstack/react-router";
import { Route as AuthedRoute } from "./_authed";

export const Route = createRoute({
  getParentRoute: () => AuthedRoute,
  path: "/insights",
  component: lazyRouteComponent(() => import("@/pages/InsightsPage")),
});
