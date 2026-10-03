// Root route: just an Outlet. Layout (nav) and the auth guard live in the
// pathless `_authed` route (src/routes/_authed.tsx) — /login must render
// WITHOUT the AppShell (no authenticated-app nav).
import { createRootRoute, Outlet } from "@tanstack/react-router";

export const Route = createRootRoute({
  component: () => <Outlet />,
});
