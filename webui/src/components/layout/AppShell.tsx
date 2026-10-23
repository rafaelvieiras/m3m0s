import { useEffect, useState } from "react";
import { Link, Outlet, useRouterState } from "@tanstack/react-router";
import {
  BarChart3,
  Clock,
  Cpu,
  Eye,
  HeartPulse,
  Home,
  ListChecks,
  LogOut,
  Menu,
  Plug,
  Plus,
  Search as SearchIcon,
  X,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { ChatWidget } from "@/components/chat/ChatWidget";
import { logout } from "@/lib/api";
import { clearApiKey } from "@/lib/auth";
import { ADVANCED_ROUTES, useAdvancedMode } from "@/lib/mode";
import { cn } from "@/lib/utils";
import { useT, type TKey } from "@/i18n";
import { LanguageSwitcher } from "@/i18n/LanguageSwitcher";

type NavItem = { to: string; label: TKey; icon: React.ComponentType<{ className?: string; "data-icon"?: string }> };

// Simple mode: the four things most people need.
const primaryItems: readonly NavItem[] = [
  { to: "/home", label: "common.nav.home", icon: Home },
  { to: "/search", label: "common.nav.search", icon: SearchIcon },
  { to: "/add", label: "common.nav.add", icon: Plus },
  { to: "/connect", label: "common.nav.connect", icon: Plug },
];

// Advanced mode adds these.
const advancedItems: readonly NavItem[] = [
  { to: "/recent", label: "common.nav.recent", icon: Clock },
  { to: "/stats", label: "common.nav.stats", icon: BarChart3 },
  { to: "/insights", label: "common.nav.insights", icon: HeartPulse },
  { to: "/jobs", label: "common.nav.jobs", icon: ListChecks },
  { to: "/embedding", label: "common.nav.embedding", icon: Cpu },
  { to: "/preview", label: "common.nav.preview", icon: Eye },
];

function isActive(pathname: string, to: string) {
  return pathname === to || pathname.startsWith(`${to}/`);
}

function ModeSwitch({ className }: { className?: string }) {
  const { t } = useT();
  const [advanced, setAdvanced] = useAdvancedMode();
  return (
    <button
      type="button"
      role="switch"
      aria-checked={advanced}
      onClick={() => setAdvanced(!advanced)}
      className={cn(
        "inline-flex items-center gap-2 rounded-md px-1.5 py-1 text-sm text-muted-foreground outline-none hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50",
        className,
      )}
    >
      <span
        aria-hidden
        className={cn(
          "relative inline-block h-4 w-7 shrink-0 rounded-full transition-colors",
          advanced ? "bg-primary" : "bg-input",
        )}
      >
        <span
          className={cn(
            "absolute top-0.5 size-3 rounded-full bg-background transition-all",
            advanced ? "left-3.5" : "left-0.5",
          )}
        />
      </span>
      <span className="whitespace-nowrap">{t("common.mode.advanced")}</span>
    </button>
  );
}

function NavLinks({ items, pathname, onNavigate, className }: {
  items: readonly NavItem[];
  pathname: string;
  onNavigate?: () => void;
  className?: string;
}) {
  const { t } = useT();
  return (
    <>
      {items.map(({ to, label, icon: Icon }) => (
        <Button
          key={to}
          render={<Link to={to} onClick={onNavigate} />}
          variant={isActive(pathname, to) ? "secondary" : "ghost"}
          size="sm"
          className={cn("whitespace-nowrap", className)}
          aria-current={isActive(pathname, to) ? "page" : undefined}
        >
          <Icon data-icon="inline-start" />
          {t(label)}
        </Button>
      ))}
    </>
  );
}

export function AppShell() {
  const { t } = useT();
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const [advanced, setAdvanced] = useAdvancedMode();
  const [menuOpen, setMenuOpen] = useState(false);

  // The mobile menu closes itself whenever the route changes.
  useEffect(() => {
    setMenuOpen(false);
  }, [pathname]);

  const onAdvancedScreen = ADVANCED_ROUTES.some((r) => isActive(pathname, r));

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
        <div className="mx-auto flex max-w-5xl items-center justify-between gap-3 px-4 py-2.5">
          <div className="flex items-center gap-4">
            <Link to="/home" className="text-sm font-semibold tracking-tight">
              {t("common.appName")}
            </Link>
            <nav aria-label={t("common.nav.label")} className="hidden items-center gap-1 md:flex">
              <NavLinks items={primaryItems} pathname={pathname} />
            </nav>
          </div>
          <div className="hidden items-center gap-2 md:flex">
            <ModeSwitch />
            <LanguageSwitcher />
            <Button variant="ghost" size="sm" onClick={handleLogout}>
              <LogOut data-icon="inline-start" />
              {t("common.logout")}
            </Button>
          </div>
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="md:hidden"
            aria-expanded={menuOpen}
            aria-controls="mobile-menu"
            aria-label={menuOpen ? t("common.menu.close") : t("common.menu.open")}
            onClick={() => setMenuOpen((o) => !o)}
          >
            {menuOpen ? <X /> : <Menu />}
          </Button>
        </div>

        {advanced && (
          <div className="hidden border-t bg-muted/30 md:block">
            <nav
              aria-label={t("common.nav.advancedLabel")}
              className="mx-auto flex max-w-5xl flex-wrap items-center gap-1 px-4 py-1.5"
            >
              <NavLinks items={advancedItems} pathname={pathname} />
            </nav>
          </div>
        )}

        {menuOpen && (
          <div id="mobile-menu" className="border-t md:hidden">
            <nav aria-label={t("common.nav.label")} className="mx-auto flex max-w-5xl flex-col gap-1 px-4 py-2">
              <NavLinks items={primaryItems} pathname={pathname} className="justify-start" />
              {advanced && (
                <>
                  <p className="mt-2 px-1 text-xs font-medium text-muted-foreground">{t("common.nav.advancedLabel")}</p>
                  <NavLinks items={advancedItems} pathname={pathname} className="justify-start" />
                </>
              )}
            </nav>
            <div className="mx-auto flex max-w-5xl flex-wrap items-center justify-between gap-2 border-t px-4 py-2">
              <ModeSwitch />
              <div className="flex items-center gap-2">
                <LanguageSwitcher />
                <Button variant="ghost" size="sm" onClick={handleLogout}>
                  <LogOut data-icon="inline-start" />
                  {t("common.logout")}
                </Button>
              </div>
            </div>
          </div>
        )}
      </header>
      <main className="mx-auto max-w-5xl px-4 py-6">
        {!advanced && onAdvancedScreen && (
          <div
            role="note"
            className="mb-4 flex flex-wrap items-center justify-between gap-2 rounded-lg border bg-muted/40 px-3 py-2 text-sm"
          >
            <span>{t("common.mode.notice")}</span>
            <Button size="sm" variant="outline" onClick={() => setAdvanced(true)}>
              {t("common.mode.switchOn")}
            </Button>
          </div>
        )}
        <Outlet />
      </main>
      <ChatWidget />
    </div>
  );
}
