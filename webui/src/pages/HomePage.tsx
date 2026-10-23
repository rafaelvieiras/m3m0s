// Home: says what M3m0s is, shows a "Getting started" checklist driven by real
// state (memories saved, API keys, embeddings) and teaches through its empty
// state. The built-in documentation memories ("selfdocs") are not counted as
// the user's own.
import { Link } from "@tanstack/react-router";
import { CheckCircle2, Circle, Plug, Plus, Search as SearchIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { PageHeader } from "@/components/layout/PageHeader";
import { useEmbeddingConfig } from "@/hooks/useEmbeddingApi";
import { useStats } from "@/hooks/useMemoryApi";
import { useT, type TKey } from "@/i18n";
import { cn } from "@/lib/utils";

interface Step {
  id: string;
  done: boolean;
  title: TKey;
  body: TKey;
  action?: { label: TKey; to: "/add" | "/connect" | "/embedding" };
}

function StepRow({ step }: { step: Step }) {
  const { t } = useT();
  return (
    <li className="flex items-start gap-3 py-2">
      {step.done ? (
        <CheckCircle2 className="mt-0.5 size-5 shrink-0 text-emerald-600 dark:text-emerald-400" aria-hidden />
      ) : (
        <Circle className="mt-0.5 size-5 shrink-0 text-muted-foreground" aria-hidden />
      )}
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <p className={cn("text-sm font-medium", step.done && "text-muted-foreground line-through")}>
          {t(step.title)}
          {step.done && <span className="sr-only"> ({t("home.done")})</span>}
        </p>
        <p className="text-sm text-muted-foreground">{t(step.body)}</p>
        {!step.done && step.action && (
          <div className="pt-1">
            <Button size="sm" variant="outline" render={<Link to={step.action.to} />}>
              {t(step.action.label)}
            </Button>
          </div>
        )}
      </div>
    </li>
  );
}

function QuickAction({ to, icon, label }: { to: "/search" | "/add" | "/connect"; icon: ReactNode; label: string }) {
  return (
    <Button variant="outline" className="h-auto justify-start gap-2 px-3 py-3 whitespace-normal" render={<Link to={to} />}>
      {icon}
      <span className="text-left">{label}</span>
    </Button>
  );
}

const NON_AGENT_SOURCES = new Set(["selfdocs", "manual", "webui", "webui-chat", ""]);

export default function HomePage() {
  const { t, tag } = useT();
  const stats = useStats();
  const embedding = useEmbeddingConfig();

  if (stats.isLoading) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-16 w-full" />
        <Skeleton className="h-56 w-full" />
      </div>
    );
  }

  // useStats() already leaves the built-in docs out of `count`.
  const mine = stats.data?.count ?? 0;
  const docs = stats.data?.countBySource?.selfdocs ?? 0;
  // An agent counts as connected once a memory arrived from a source other
  // than the built-in docs, the WebUI itself or manual entry.
  const sources = Object.entries(stats.data?.countBySource ?? {});
  const agentConnected = sources.some(([name, n]) => n > 0 && !NON_AGENT_SOURCES.has(name));

  const steps: Step[] = [
    { id: "account", done: true, title: "home.step.account.title", body: "home.step.account.body" },
    {
      id: "memory",
      done: mine > 0,
      title: "home.step.memory.title",
      body: "home.step.memory.body",
      action: { label: "home.step.memory.action", to: "/add" },
    },
    {
      id: "agent",
      done: agentConnected,
      title: "home.step.agent.title",
      body: "home.step.agent.body",
      action: { label: "home.step.agent.action", to: "/connect" },
    },
    {
      id: "search",
      done: embedding.data?.active === true,
      title: "home.step.search.title",
      body: "home.step.search.body",
      action: { label: "home.step.search.action", to: "/embedding" },
    },
  ];
  const doneCount = steps.filter((s) => s.done).length;

  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={t("home.title")} subtitle={t("home.subtitle")} help="home" term="memory" />

      {mine === 0 ? (
        <Card className="border-dashed">
          <CardHeader>
            <CardTitle className="text-base">{t("home.empty.title")}</CardTitle>
            <CardDescription>{t("home.empty.body")}</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <p className="text-sm text-muted-foreground italic">{t("home.empty.example")}</p>
            <div>
              <Button render={<Link to="/add" />}>
                <Plus data-icon="inline-start" />
                {t("home.step.memory.action")}
              </Button>
            </div>
          </CardContent>
        </Card>
      ) : (
        <p className="text-sm">
          {t("home.count", { count: mine })}
          {docs > 0 && <span className="text-muted-foreground"> {t("home.docsNote", { count: new Intl.NumberFormat(tag).format(docs) })}</span>}
        </p>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("home.checklist.title")}</CardTitle>
          <CardDescription>{t("home.checklist.progress", { done: doneCount, total: steps.length })}</CardDescription>
        </CardHeader>
        <CardContent>
          {doneCount === steps.length && <p className="pb-2 text-sm text-muted-foreground">{t("home.checklist.allDone")}</p>}
          <ol className="divide-y">
            {steps.map((s) => (
              <StepRow key={s.id} step={s} />
            ))}
          </ol>
        </CardContent>
      </Card>

      <div>
        <h2 className="mb-2 text-sm font-medium">{t("home.quick.title")}</h2>
        <div className="grid gap-2 sm:grid-cols-3">
          <QuickAction to="/search" icon={<SearchIcon className="size-4 shrink-0" />} label={t("home.quick.search")} />
          <QuickAction to="/add" icon={<Plus className="size-4 shrink-0" />} label={t("home.quick.add")} />
          <QuickAction to="/connect" icon={<Plug className="size-4 shrink-0" />} label={t("home.quick.connect")} />
        </div>
      </div>
    </div>
  );
}
