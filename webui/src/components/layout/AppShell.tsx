import { Link, Outlet, useRouterState } from "@tanstack/react-router";
import { LogOut, Search as SearchIcon, Clock, BarChart3, Plus, ListChecks, Eye, Cpu, HeartPulse } from "lucide-react";
import { Button } from "@/components/ui/button";
import { ChatWidget } from "@/components/chat/ChatWidget";
import { logout } from "@/lib/api";
import { clearApiKey } from "@/lib/auth";
import { useT, type TKey } from "@/i18n";
import { LanguageSwitcher } from "@/i18n/LanguageSwitcher";

const navItems = [
  { to: "/search", label: "common.nav.search", icon: SearchIcon },
  { to: "/recent", label: "common.nav.recent", icon: Clock },
  { to: "/stats", label: "common.nav.stats", icon: BarChart3 },
  { to: "/insights", label: "common.nav.insights", icon: HeartPulse },
  { to: "/add", label: "common.nav.add", icon: Plus },
  { to: "/jobs", label: "common.nav.jobs", icon: ListChecks },
  { to: "/embedding", label: "common.nav.embedding", icon: Cpu },
  { to: "/preview", label: "common.nav.preview", icon: Eye },
] as const satisfies readonly { to: string; label: TKey; icon: unknown }[];

export function AppShell() {
  const { t } = useT();
  const pathname = useRouterState({ select: (s) => s.location.pathname });

  async function handleLogout() {
    clearApiKey();
    try {
      await logout(); // revokes the cookie session; harmless for API-key sign-ins
    } catch {
      // the server being unreachable must not trap the user in the app
    }
    window.location.assign("/login");
  }

  return (
    <div className="min-h-svh bg-background text-foreground">
      <header className="border-b">
        <div className="mx-auto flex max-w-5xl items-center justify-between gap-4 px-4 py-3">
          <span className="text-sm font-semibold tracking-tight">{t("common.appName")}</span>
          <nav className="flex flex-wrap items-center gap-1">
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
                  {t(label)}
                </Button>
              );
            })}
          </nav>
          <div className="flex items-center gap-2">
            <LanguageSwitcher />
            <Button variant="ghost" size="sm" onClick={handleLogout}>
              <LogOut data-icon="inline-start" />
              {t("common.logout")}
            </Button>
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-5xl px-4 py-6">
        <Outlet />
      </main>
      <ChatWidget />
    </div>
  );
}
