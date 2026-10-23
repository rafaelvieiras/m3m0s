import { createRoute, lazyRouteComponent, redirect } from "@tanstack/react-router";
import { Route as RootRoute } from "./__root";
import { isSignedIn } from "@/lib/session";

export const Route = createRoute({
  getParentRoute: () => RootRoute,
  path: "/login",
  beforeLoad: async () => {
    if (await isSignedIn()) {
      throw redirect({ to: "/home" });
    }
  },
  component: lazyRouteComponent(() => import("@/pages/LoginPage")),
});
