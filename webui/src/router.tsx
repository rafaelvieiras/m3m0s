// Builds the route tree (code-based routing, no file generation —
// docs/implementation-plan.md Phase 5 only requires "real routes" +
// code-splitting via lazyRouteComponent, not TanStack Router's file-based
// codegen).
import { createRouter } from "@tanstack/react-router";
import { Route as RootRoute } from "./routes/__root";
import { Route as LoginRoute } from "./routes/login";
import { Route as ResetPasswordRoute } from "./routes/reset-password";
import { Route as IndexRoute } from "./routes/index";
import { Route as AuthedRoute } from "./routes/_authed";
import { Route as SearchRoute } from "./routes/search";
import { Route as RecentRoute } from "./routes/recent";
import { Route as StatsRoute } from "./routes/stats";
import { Route as AddRoute } from "./routes/add";
import { Route as EntryDetailRoute } from "./routes/entry-detail";
import { Route as EntryEditRoute } from "./routes/entry-edit";
import { Route as JobsRoute } from "./routes/jobs";
import { Route as EmbeddingRoute } from "./routes/embedding";
import { Route as PreviewRoute } from "./routes/preview";
import { Route as InsightsRoute } from "./routes/insights";
import { Route as HomeRoute } from "./routes/home";
import { Route as ConnectRoute } from "./routes/connect";
import { Route as TrashRoute } from "./routes/trash";
import { NotFound } from "@/components/layout/NotFound";

const routeTree = RootRoute.addChildren([
  LoginRoute,
  ResetPasswordRoute,
  IndexRoute,
  AuthedRoute.addChildren([
    SearchRoute,
    RecentRoute,
    StatsRoute,
    AddRoute,
    EntryDetailRoute,
    EntryEditRoute,
    JobsRoute,
    EmbeddingRoute,
    PreviewRoute,
    InsightsRoute,
    HomeRoute,
    ConnectRoute,
    TrashRoute,
  ]),
]);

export const router = createRouter({ routeTree, defaultPreload: "intent", defaultNotFoundComponent: NotFound });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
