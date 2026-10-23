// Home: the landing page after sign-in (what M3m0s is + "Getting started").
import { createRoute, lazyRouteComponent } from "@tanstack/react-router";
import { Route as AuthedRoute } from "./_authed";

export const Route = createRoute({
  getParentRoute: () => AuthedRoute,
  path: "/home",
  component: lazyRouteComponent(() => import("@/pages/HomePage")),
});
