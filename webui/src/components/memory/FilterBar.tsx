// Filters on the 4 schema axes (D9/docs/memory-schema.md): domain,
// domainCategory, source, agent, model, project — plus tags (semantic search only, D11 does
// not include tags in /recent). All are free-form strings (not a closed
// enum), hence Input instead of Select.
import { Search as SearchIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";

export interface FilterValues {
  domain: string;
  domainCategory: string;
  source: string;
  agent: string;
  model: string;
  project: string;
  tags: string;
}

export const emptyFilters: FilterValues = {
  domain: "",
  domainCategory: "",
  source: "",
  agent: "",
  model: "",
  project: "",
  tags: "",
};

interface FilterBarProps {
  value: FilterValues;
  onChange: (value: FilterValues) => void;
  onApply: () => void;
  showTags?: boolean;
}

export function FilterBar({ value, onChange, onApply, showTags = false }: FilterBarProps) {
  function set<K extends keyof FilterValues>(key: K, v: FilterValues[K]) {
    onChange({ ...value, [key]: v });
  }

  return (
    <form
      className="rounded-lg border bg-card p-4"
      onSubmit={(e) => {
        e.preventDefault();
        onApply();
      }}
    >
      <FieldGroup className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
        <Field>
          <FieldLabel htmlFor="filter-domain">domain</FieldLabel>
          <Input
            id="filter-domain"
            placeholder="code:m3m0s"
            value={value.domain}
            onChange={(e) => set("domain", e.target.value)}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="filter-domain-category">domainCategory</FieldLabel>
          <Input
            id="filter-domain-category"
            placeholder="code"
            value={value.domainCategory}
            onChange={(e) => set("domainCategory", e.target.value)}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="filter-source">source</FieldLabel>
          <Input
            id="filter-source"
            placeholder="claude-code"
            value={value.source}
            onChange={(e) => set("source", e.target.value)}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="filter-agent">agent</FieldLabel>
          <Input
            id="filter-agent"
            placeholder="my-agent"
            value={value.agent}
            onChange={(e) => set("agent", e.target.value)}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="filter-model">model</FieldLabel>
          <Input
            id="filter-model"
            placeholder="claude-sonnet-5-5"
            value={value.model}
            onChange={(e) => set("model", e.target.value)}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="filter-project">project</FieldLabel>
          <Input
            id="filter-project"
            placeholder="m3m0s"
            value={value.project}
            onChange={(e) => set("project", e.target.value)}
          />
        </Field>
        {showTags && (
          <Field>
            <FieldLabel htmlFor="filter-tags">tags (comma-separated)</FieldLabel>
            <Input
              id="filter-tags"
              placeholder="chapter-3,draft"
              value={value.tags}
              onChange={(e) => set("tags", e.target.value)}
            />
          </Field>
        )}
      </FieldGroup>
      <div className="mt-3 flex justify-end">
        <Button type="submit" size="sm">
          <SearchIcon data-icon="inline-start" />
          Apply filters
        </Button>
      </div>
    </form>
  );
}

export function filtersToParams(f: FilterValues) {
  return {
    domain: f.domain.trim() || undefined,
    domainCategory: f.domainCategory.trim() || undefined,
    source: f.source.trim() || undefined,
    agent: f.agent.trim() || undefined,
    model: f.model.trim() || undefined,
    project: f.project.trim() || undefined,
  };
}

export function tagsStringToArray(tags: string): string[] | undefined {
  const arr = tags
    .split(",")
    .map((t) => t.trim())
    .filter(Boolean);
  return arr.length > 0 ? arr : undefined;
}
