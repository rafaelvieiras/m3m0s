import { createRoute, lazyRouteComponent, redirect } from "@tanstack/react-router";
import { Route as RootRoute } from "./__root";
import { isAuthenticated } from "@/lib/auth";

export const Route = createRoute({
  getParentRoute: () => RootRoute,
  path: "/login",
  beforeLoad: () => {
    if (isAuthenticated()) {
      throw redirect({ to: "/search" });
    }
  },
  component: lazyRouteComponent(() => import("@/pages/LoginPage")),
});
