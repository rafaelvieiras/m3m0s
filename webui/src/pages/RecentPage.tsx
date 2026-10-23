// GET /recent — the newest memories, with the same filters as search (except
// tags/query, D11/contract).
import { useState } from "react";
import { useT } from "@/i18n";
import { Route } from "@/routes/recent";
import { Link } from "@tanstack/react-router";
import { Loader2, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { PageHeader } from "@/components/layout/PageHeader";
import { FilterBar, filtersToParams, type FilterValues } from "@/components/memory/FilterBar";
import { EntryList } from "@/components/memory/EntryList";
import { useRecent } from "@/hooks/useMemoryApi";

export default function RecentPage() {
  const { t } = useT();
  const search = Route.useSearch();
  const navigate = Route.useNavigate();

  const [filters, setFilters] = useState<FilterValues>({
    domain: search.domain,
    domainCategory: search.domainCategory,
    source: search.source,
    agent: search.agent,
    model: search.model,
    project: search.project,
    scope: search.scope,
    tags: "",
    includeDocs: search.docs,
  });

  function applyFilters() {
    navigate({
      search: {
        domain: filters.domain,
        domainCategory: filters.domainCategory,
        source: filters.source,
        agent: filters.agent,
        model: filters.model,
        project: filters.project,
        scope: filters.scope,
        docs: filters.includeDocs,
      },
    });
  }

  const { data, isLoading, isError, error } = useRecent({
    limit: 50,
    ...filtersToParams({
      domain: search.domain,
      domainCategory: search.domainCategory,
      source: search.source,
      agent: search.agent,
      model: search.model,
      project: search.project,
      scope: search.scope,
      tags: "",
      includeDocs: search.docs,
    }),
  });

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title={t("memory.recent.title")}
        subtitle={t("memory.recent.subtitle")}
        help="recent"
        actions={isLoading ? <Loader2 className="size-4 animate-spin text-muted-foreground" /> : undefined}
      />

      <FilterBar value={filters} onChange={setFilters} onApply={applyFilters} />

      {isLoading && (
        <div className="flex flex-col gap-2">
          <Skeleton className="h-20 w-full" />
          <Skeleton className="h-20 w-full" />
          <Skeleton className="h-20 w-full" />
        </div>
      )}

      {isError && <p className="text-sm text-destructive">{t("memory.recent.failed", { message: error.message })}</p>}

      {data && (
        <EntryList
          entries={data.results}
          emptyTitle={t("memory.recent.emptyTitle")}
          emptyLabel={t("memory.recent.empty")}
          emptyAction={
            <Button variant="outline" size="sm" render={<Link to="/add" />}>
              <Plus data-icon="inline-start" />
              {t("memory.search.emptyAction")}
            </Button>
          }
        />
      )}
    </div>
  );
}
