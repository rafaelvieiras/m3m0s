// Pathless layout that protects all authenticated routes (D13, item 2):
// beforeLoad redirects to /login if there is no saved API key; component
// mounts the AppShell (nav) around the child routes' <Outlet/>.
import { createRoute, redirect } from "@tanstack/react-router";
import { Route as RootRoute } from "./__root";
import { isAuthenticated } from "@/lib/auth";
import { AppShell } from "@/components/layout/AppShell";

export const Route = createRoute({
  getParentRoute: () => RootRoute,
  id: "_authed",
  beforeLoad: () => {
    if (!isAuthenticated()) {
      throw redirect({ to: "/login" });
    }
  },
  component: AppShell,
});
