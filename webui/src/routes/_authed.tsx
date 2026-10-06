// Pathless layout that protects all authenticated routes (D13, item 2):
// beforeLoad redirects to /login if there is neither a saved API key nor a valid session; component
// mounts the AppShell (nav) around the child routes' <Outlet/>.
import { createRoute, redirect } from "@tanstack/react-router";
import { Route as RootRoute } from "./__root";
import { isSignedIn } from "@/lib/session";
import { AppShell } from "@/components/layout/AppShell";

export const Route = createRoute({
  getParentRoute: () => RootRoute,
  id: "_authed",
  beforeLoad: async () => {
    if (!(await isSignedIn())) {
      throw redirect({ to: "/login" });
    }
  },
  component: AppShell,
});
