// Connect agent: server URL, API key guidance and ready-to-paste setups.
import { createRoute, lazyRouteComponent } from "@tanstack/react-router";
import { Route as AuthedRoute } from "./_authed";

export const Route = createRoute({
  getParentRoute: () => AuthedRoute,
  path: "/connect",
  component: lazyRouteComponent(() => import("@/pages/ConnectPage")),
});
