// Shown for any URL that matches no route (router defaultNotFoundComponent).
// Lives outside the AppShell so it also works when signed out.
import { Link } from "@tanstack/react-router";
import { Home } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { LanguageSwitcher } from "@/i18n/LanguageSwitcher";
import { useT } from "@/i18n";

export function NotFound() {
  const { t } = useT();
  return (
    <main className="relative flex min-h-svh items-center justify-center bg-background px-4">
      <LanguageSwitcher className="absolute top-4 right-4" />
      <Empty className="max-w-md border">
        <EmptyHeader>
          <EmptyTitle as="h1">{t("common.notFound.title")}</EmptyTitle>
          <EmptyDescription>{t("common.notFound.hint")}</EmptyDescription>
        </EmptyHeader>
        <Button variant="outline" render={<Link to="/" />}>
          <Home data-icon="inline-start" />
          {t("common.notFound.home")}
        </Button>
      </Empty>
    </main>
  );
}
