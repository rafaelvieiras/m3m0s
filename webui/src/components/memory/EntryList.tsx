// Virtualized list (TanStack Virtual) of search/recent results — it can grow
// large (docs/implementation-plan.md, Phase 5). Row height is variable
// (truncated content + badges), hence `measureElement` instead of a fixed
// estimateSize.
import { useRef } from "react";
import { useT } from "@/i18n";
import { useVirtualizer } from "@tanstack/react-virtual";
import { Link } from "@tanstack/react-router";
import { Badge } from "@/components/ui/badge";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import type { Entry } from "@/lib/schemas";

const MAX_SNIPPET_CHARS = 220;

interface EntryListProps {
  entries: Entry[];
  emptyLabel?: string;
}

export function EntryList({ entries, emptyLabel }: EntryListProps) {
  const { t } = useT();
  const parentRef = useRef<HTMLDivElement>(null);

  const virtualizer = useVirtualizer({
    count: entries.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 96,
    overscan: 8,
  });

  if (entries.length === 0) {
    return (
      <Empty className="border">
        <EmptyHeader>
          <EmptyTitle>{t("memory.list.emptyTitle")}</EmptyTitle>
          <EmptyDescription>{emptyLabel ?? t("memory.list.empty")}</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }

  return (
    <div ref={parentRef} className="max-h-[70vh] overflow-y-auto rounded-lg border">
      <div
        style={{ height: `${virtualizer.getTotalSize()}px`, position: "relative", width: "100%" }}
      >
        {virtualizer.getVirtualItems().map((virtualRow) => {
          const entry = entries[virtualRow.index];
          return (
            <div
              key={entry.id}
              data-index={virtualRow.index}
              ref={virtualizer.measureElement}
              style={{
                position: "absolute",
                top: 0,
                left: 0,
                width: "100%",
                transform: `translateY(${virtualRow.start}px)`,
              }}
              className={virtualRow.index > 0 ? "border-t" : ""}
            >
              <EntryRow entry={entry} />
            </div>
          );
        })}
      </div>
    </div>
  );
}

function EntryRow({ entry }: { entry: Entry }) {
  const { t, tag } = useT();
  const snippet =
    entry.content.length > MAX_SNIPPET_CHARS
      ? `${entry.content.slice(0, MAX_SNIPPET_CHARS)}…`
      : entry.content;
  const filePath = entry.metadata.file_path;
  const heading = entry.metadata.heading;

  return (
    <Link
      to="/entries/$id"
      params={{ id: entry.id }}
      className="block px-4 py-3 transition-colors hover:bg-muted/50"
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex flex-wrap items-center gap-1.5">
          <Badge variant="secondary">{entry.domain}</Badge>
          <Badge variant="outline">{entry.source}</Badge>
          {entry.agent && <Badge variant="outline">@{entry.agent}</Badge>}
          {entry.tags.map((tag) => (
            <Badge key={tag} variant="outline" className="text-muted-foreground">
              #{tag}
            </Badge>
          ))}
        </div>
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          {entry.score !== undefined && <span>{t("memory.list.score", { score: entry.score.toFixed(2) })}</span>}
          <span>{new Date(entry.createdAt).toLocaleString(tag)}</span>
        </div>
      </div>
      <p className="mt-1.5 text-sm text-foreground">{snippet}</p>
      {filePath && (
        <p className="mt-1 text-xs text-muted-foreground">
          — {filePath}
          {heading ? ` § ${heading}` : ""}
        </p>
      )}
    </Link>
  );
}
