import { Link, Outlet, useRouterState } from "@tanstack/react-router";
import { LogOut, Search as SearchIcon, Clock, BarChart3, Plus, ListChecks, Eye } from "lucide-react";
import { Button } from "@/components/ui/button";
import { ChatWidget } from "@/components/chat/ChatWidget";
import { clearApiKey } from "@/lib/auth";

const navItems = [
  { to: "/search", label: "Search", icon: SearchIcon },
  { to: "/recent", label: "Recent", icon: Clock },
  { to: "/stats", label: "Stats", icon: BarChart3 },
  { to: "/add", label: "Add", icon: Plus },
  { to: "/jobs", label: "Jobs", icon: ListChecks },
  { to: "/preview", label: "Preview", icon: Eye },
] as const;

export function AppShell() {
  const pathname = useRouterState({ select: (s) => s.location.pathname });

  function handleLogout() {
    clearApiKey();
    window.location.assign("/login");
  }

  return (
    <div className="min-h-svh bg-background text-foreground">
      <header className="border-b">
        <div className="mx-auto flex max-w-5xl items-center justify-between gap-4 px-4 py-3">
          <span className="text-sm font-semibold tracking-tight">M3m0s</span>
          <nav className="flex items-center gap-1">
            {navItems.map(({ to, label, icon: Icon }) => {
              const active = pathname === to || pathname.startsWith(`${to}/`);
              return (
                <Button
                  key={to}
                  render={<Link to={to} />}
                  variant={active ? "secondary" : "ghost"}
                  size="sm"
                >
                  <Icon data-icon="inline-start" />
                  {label}
                </Button>
              );
            })}
          </nav>
          <Button variant="ghost" size="sm" onClick={handleLogout}>
            <LogOut data-icon="inline-start" />
            Log out
          </Button>
        </div>
      </header>
      <main className="mx-auto max-w-5xl px-4 py-6">
        <Outlet />
      </main>
      <ChatWidget />
    </div>
  );
}
