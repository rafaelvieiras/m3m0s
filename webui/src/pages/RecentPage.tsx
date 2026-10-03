// GET /recent — same filters as search (except tags/query, D11/contract).
import { useState } from "react";
import { Route } from "@/routes/recent";
import { Loader2 } from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";
import { FilterBar, type FilterValues } from "@/components/memory/FilterBar";
import { EntryList } from "@/components/memory/EntryList";
import { useRecent } from "@/hooks/useMemoryApi";

export default function RecentPage() {
  const search = Route.useSearch();
  const navigate = Route.useNavigate();

  const [filters, setFilters] = useState<FilterValues>({
    domain: search.domain,
    domainCategory: search.domainCategory,
    source: search.source,
    agent: search.agent,
    tags: "",
  });

  function applyFilters() {
    navigate({
      search: {
        domain: filters.domain,
        domainCategory: filters.domainCategory,
        source: filters.source,
        agent: filters.agent,
      },
    });
  }

  const { data, isLoading, isError, error } = useRecent({
    limit: 50,
    domain: search.domain || undefined,
    domainCategory: search.domainCategory || undefined,
    source: search.source || undefined,
    agent: search.agent || undefined,
  });

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-lg font-semibold">Recent memories</h1>
          <p className="text-sm text-muted-foreground">
            Sorted by creation date (GET /recent), no semantic search.
          </p>
        </div>
        {isLoading && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
      </div>

      <FilterBar value={filters} onChange={setFilters} onApply={applyFilters} />

      {isLoading && (
        <div className="flex flex-col gap-2">
          <Skeleton className="h-20 w-full" />
          <Skeleton className="h-20 w-full" />
          <Skeleton className="h-20 w-full" />
        </div>
      )}

      {isError && <p className="text-sm text-destructive">Failed to load: {error.message}</p>}

      {data && <EntryList entries={data.results} emptyLabel="No recent memories match these filters." />}
    </div>
  );
}
