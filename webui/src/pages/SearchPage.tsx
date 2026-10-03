// Semantic search (POST /search) — query + filters on the 4 axes, all
// reflected in the URL (route search params, see src/routes/search.tsx) so
// the screen is back-navigable/shareable.
import { useState } from "react";
import { Route } from "@/routes/search";
import { Search as SearchIcon, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { FilterBar, filtersToParams, tagsStringToArray, type FilterValues } from "@/components/memory/FilterBar";
import { EntryList } from "@/components/memory/EntryList";
import { useSearchMemories } from "@/hooks/useMemoryApi";

export default function SearchPage() {
  const search = Route.useSearch();
  const navigate = Route.useNavigate();

  const [queryInput, setQueryInput] = useState(search.q);
  const [filters, setFilters] = useState<FilterValues>({
    domain: search.domain,
    domainCategory: search.domainCategory,
    source: search.source,
    agent: search.agent,
    tags: search.tags,
  });

  function applyFilters(nextQuery = queryInput, nextFilters: FilterValues = filters) {
    navigate({
      search: {
        q: nextQuery,
        domain: nextFilters.domain,
        domainCategory: nextFilters.domainCategory,
        source: nextFilters.source,
        agent: nextFilters.agent,
        tags: nextFilters.tags,
      },
    });
  }

  const params =
    search.q.trim().length > 0
      ? {
          query: search.q,
          ...filtersToParams({
            domain: search.domain,
            domainCategory: search.domainCategory,
            source: search.source,
            agent: search.agent,
            tags: search.tags,
          }),
          tags: tagsStringToArray(search.tags),
          limit: 30,
        }
      : null;

  const { data, isLoading, isError, error } = useSearchMemories(params);

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h1 className="text-lg font-semibold">Semantic search</h1>
        <p className="text-sm text-muted-foreground">
          Vector search against the M3m0s index (POST /search).
        </p>
      </div>

      <form
        className="flex gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          applyFilters();
        }}
      >
        <FieldGroup className="flex-1">
          <Field>
            <FieldLabel htmlFor="search-query" className="sr-only">
              Query
            </FieldLabel>
            <Input
              id="search-query"
              placeholder="What are you looking for?"
              value={queryInput}
              onChange={(e) => setQueryInput(e.target.value)}
              autoFocus
            />
          </Field>
        </FieldGroup>
        <Button type="submit" disabled={!queryInput.trim()}>
          {isLoading ? (
            <Loader2 data-icon="inline-start" className="animate-spin" />
          ) : (
            <SearchIcon data-icon="inline-start" />
          )}
          Search
        </Button>
      </form>

      <FilterBar
        value={filters}
        onChange={setFilters}
        onApply={() => applyFilters(queryInput, filters)}
        showTags
      />

      {params === null && (
        <p className="text-sm text-muted-foreground">Type a query and click Search.</p>
      )}

      {params !== null && isLoading && (
        <div className="flex flex-col gap-2">
          <Skeleton className="h-20 w-full" />
          <Skeleton className="h-20 w-full" />
          <Skeleton className="h-20 w-full" />
        </div>
      )}

      {isError && (
        <p className="text-sm text-destructive">Search failed: {error.message}</p>
      )}

      {data && <EntryList entries={data.results} emptyLabel="No results for this search/filters." />}
    </div>
  );
}
