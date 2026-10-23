// Filters for the memory lists. By default only Topic and Project are shown;
// the rest sit under "More filters" (Advanced mode only), and on small screens
// the whole bar collapses behind a "Filters (n)" button.
// Values are free-form strings (not closed enums), hence Input instead of Select.
import { useState } from "react";
import { ChevronDown, Search as SearchIcon, SlidersHorizontal } from "lucide-react";
import { useT } from "@/i18n";
import { Button } from "@/components/ui/button";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { HelpTip } from "@/components/help/HelpTip";
import { useAdvancedMode } from "@/lib/mode";

export interface FilterValues {
  domain: string;
  domainCategory: string;
  source: string;
  agent: string;
  model: string;
  project: string;
  scope: "" | "global" | "project-only";
  tags: string;
  /** Show the ~100 built-in documentation pages that ship with M3m0s. */
  includeDocs: boolean;
}

export const emptyFilters: FilterValues = {
  domain: "",
  domainCategory: "",
  source: "",
  agent: "",
  model: "",
  project: "",
  scope: "",
  tags: "",
  includeDocs: false,
};

/** How many filters are set (the number shown in "Filters (n)"). */
export function activeFilterCount(f: FilterValues, showTags: boolean): number {
  const fields = [f.domain, f.domainCategory, f.source, f.agent, f.model, f.project, f.scope];
  if (showTags) fields.push(f.tags);
  return fields.filter((v) => v.trim() !== "").length + (f.includeDocs ? 1 : 0);
}

interface FilterBarProps {
  value: FilterValues;
  onChange: (value: FilterValues) => void;
  onApply: () => void;
  showTags?: boolean;
}

export function FilterBar({ value, onChange, onApply, showTags = false }: FilterBarProps) {
  const { t } = useT();
  const [advanced] = useAdvancedMode();
  const [open, setOpen] = useState(false);
  const count = activeFilterCount(value, showTags);
  const hasMoreValues =
    [value.domainCategory, value.source, value.agent, value.model, value.scope].some((v) => v !== "") ||
    (showTags && value.tags !== "") ||
    value.includeDocs;

  function set<K extends keyof FilterValues>(key: K, v: FilterValues[K]) {
    onChange({ ...value, [key]: v });
  }

  return (
    <form
      className="rounded-lg border bg-card p-3 sm:p-4"
      onSubmit={(e) => {
        e.preventDefault();
        onApply();
      }}
    >
      <button
        type="button"
        className="flex w-full items-center justify-between gap-2 text-sm font-medium sm:hidden"
        aria-expanded={open}
        aria-controls="filter-panel"
        onClick={() => setOpen((o) => !o)}
      >
        <span className="inline-flex items-center gap-1.5">
          <SlidersHorizontal className="size-4" aria-hidden />
          {count > 0 ? t("memory.filter.titleCount", { count }) : t("memory.filter.title")}
        </span>
        <ChevronDown className={`size-4 transition-transform ${open ? "rotate-180" : ""}`} aria-hidden />
      </button>

      <div id="filter-panel" className={`${open ? "mt-3 block" : "hidden"} sm:mt-0 sm:block`}>
        <FieldGroup className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="filter-domain">
              {t("common.field.domain")}
              <HelpTip term="domain" />
            </FieldLabel>
            <Input
              id="filter-domain"
              placeholder="code:my-app"
              value={value.domain}
              onChange={(e) => set("domain", e.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="filter-project">
              {t("common.field.project")}
              <HelpTip term="project" />
            </FieldLabel>
            <Input
              id="filter-project"
              placeholder="my-app"
              value={value.project}
              onChange={(e) => set("project", e.target.value)}
            />
          </Field>
        </FieldGroup>

        {advanced && (
          <details className="group mt-3" open={hasMoreValues || undefined}>
            <summary className="inline-flex cursor-pointer list-none items-center gap-1 text-sm text-muted-foreground outline-none hover:text-foreground [&::-webkit-details-marker]:hidden">
              <ChevronDown className="size-4 transition-transform group-open:rotate-180" aria-hidden />
              {t("memory.filter.more")}
            </summary>
            <FieldGroup className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
              <Field>
                <FieldLabel htmlFor="filter-domain-category">{t("common.field.domainCategory")}</FieldLabel>
                <Input
                  id="filter-domain-category"
                  placeholder="code"
                  value={value.domainCategory}
                  onChange={(e) => set("domainCategory", e.target.value)}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="filter-source">{t("common.field.source")}</FieldLabel>
                <Input
                  id="filter-source"
                  placeholder="claude-code"
                  value={value.source}
                  onChange={(e) => set("source", e.target.value)}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="filter-agent">{t("common.field.agent")}</FieldLabel>
                <Input
                  id="filter-agent"
                  placeholder="my-agent"
                  value={value.agent}
                  onChange={(e) => set("agent", e.target.value)}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="filter-model">{t("common.field.model")}</FieldLabel>
                <Input
                  id="filter-model"
                  placeholder="claude-sonnet-5-5"
                  value={value.model}
                  onChange={(e) => set("model", e.target.value)}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="filter-scope">{t("memory.filter.scope")}</FieldLabel>
                <select
                  id="filter-scope"
                  className="h-8 w-full min-w-0 rounded-lg border border-input bg-transparent px-2.5 text-sm"
                  value={value.scope}
                  onChange={(e) => set("scope", e.target.value as FilterValues["scope"])}
                >
                  <option value="">{t("memory.filter.scopeAll")}</option>
                  <option value="project-only">{t("memory.filter.scopeProject")}</option>
                  <option value="global">{t("memory.filter.scopeGlobal")}</option>
                </select>
              </Field>
              {showTags && (
                <Field>
                  <FieldLabel htmlFor="filter-tags">{t("memory.filter.tags")}</FieldLabel>
                  <Input
                    id="filter-tags"
                    placeholder="chapter-3,draft"
                    value={value.tags}
                    onChange={(e) => set("tags", e.target.value)}
                  />
                </Field>
              )}
              <label
                className="flex items-start gap-2 text-sm sm:col-span-2 lg:col-span-3"
                title={t("memory.filter.includeDocsHint")}
              >
                <input
                  type="checkbox"
                  className="mt-0.5 size-4"
                  checked={value.includeDocs}
                  onChange={(e) => set("includeDocs", e.target.checked)}
                />
                <span>
                  {t("memory.filter.includeDocs")}
                  <span className="block text-xs text-muted-foreground">{t("memory.filter.includeDocsHint")}</span>
                </span>
              </label>
            </FieldGroup>
          </details>
        )}

        <div className="mt-3 flex flex-wrap justify-end gap-2">
          {count > 0 && (
            <Button type="button" size="sm" variant="ghost" onClick={() => onChange({ ...emptyFilters })}>
              {t("memory.filter.clear")}
            </Button>
          )}
          <Button type="submit" size="sm">
            <SearchIcon data-icon="inline-start" />
            {t("memory.filter.apply")}
          </Button>
        </div>
      </div>
    </form>
  );
}

export function filtersToParams(f: FilterValues) {
  const source = f.source.trim();
  return {
    domain: f.domain.trim() || undefined,
    domainCategory: f.domainCategory.trim() || undefined,
    source: source || undefined,
    // The built-in docs are hidden unless asked for (or filtered by source).
    excludeSource: f.includeDocs || source ? undefined : "selfdocs",
    agent: f.agent.trim() || undefined,
    model: f.model.trim() || undefined,
    project: f.project.trim() || undefined,
    scope: f.scope || undefined,
  };
}

export function tagsStringToArray(tags: string): string[] | undefined {
  const arr = tags
    .split(",")
    .map((t) => t.trim())
    .filter(Boolean);
  return arr.length > 0 ? arr : undefined;
}
