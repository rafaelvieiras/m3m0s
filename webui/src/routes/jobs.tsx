import { createRoute, lazyRouteComponent } from "@tanstack/react-router";
import { Route as AuthedRoute } from "./_authed";

export const Route = createRoute({
  getParentRoute: () => AuthedRoute,
  path: "/jobs",
  component: lazyRouteComponent(() => import("@/pages/JobsPage")),
});
