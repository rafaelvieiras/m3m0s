// Virtualized list (TanStack Virtual) of search/recent results — it can grow
// large (docs/implementation-plan.md, Phase 5). Row height is variable
// (truncated content + badges), hence `measureElement` instead of a fixed
// estimateSize.
import { useRef, type ReactNode } from "react";
import { useT } from "@/i18n";
import { useVirtualizer } from "@tanstack/react-virtual";
import { Link } from "@tanstack/react-router";
import { Pencil, Trash2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { DeleteEntryDialog } from "@/components/memory/DeleteEntryDialog";
import { KindBadge } from "@/components/memory/KindBadge";
import { useAdvancedMode } from "@/lib/mode";
import { formatDateTime, formatDecimal } from "@/lib/format";
import { snippet } from "@/lib/text";
import type { Entry } from "@/lib/schemas";

const MAX_SNIPPET_CHARS = 220;

interface EntryListProps {
  entries: Entry[];
  emptyTitle?: string;
  emptyLabel?: string;
  /** Primary action shown under the empty message (e.g. "Add a memory"). */
  emptyAction?: ReactNode;
}

export function EntryList({ entries, emptyTitle, emptyLabel, emptyAction }: EntryListProps) {
  const { t } = useT();
  const parentRef = useRef<HTMLDivElement>(null);

  const virtualizer = useVirtualizer({
    count: entries.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 110,
    overscan: 8,
  });

  if (entries.length === 0) {
    return (
      <Empty className="border">
        <EmptyHeader>
          <EmptyTitle>{emptyTitle ?? t("memory.list.emptyTitle")}</EmptyTitle>
          <EmptyDescription>{emptyLabel ?? t("memory.list.empty")}</EmptyDescription>
        </EmptyHeader>
        {emptyAction}
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
  const [advanced] = useAdvancedMode();
  const filePath = entry.metadata.file_path;
  const heading = entry.metadata.heading;

  return (
    <div className="px-4 py-3 transition-colors hover:bg-muted/50">
      <Link to="/entries/$id" params={{ id: entry.id }} className="block [overflow-wrap:anywhere]">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex min-w-0 flex-wrap items-center gap-1.5">
            <KindBadge kind={entry.kind} kindSource={entry.kindSource} />
            {entry.project && <Badge variant="secondary">{entry.project}</Badge>}
            <Badge variant="outline">{entry.domain}</Badge>
            {advanced && <Badge variant="outline">{entry.source}</Badge>}
            {advanced && entry.agent && <Badge variant="outline">@{entry.agent}</Badge>}
            {entry.tags.map((tagName) => (
              <Badge key={tagName} variant="outline" className="text-muted-foreground">
                #{tagName}
              </Badge>
            ))}
          </div>
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            {advanced && entry.score !== undefined && (
              <span>{t("memory.list.score", { score: formatDecimal(entry.score, tag, 2) })}</span>
            )}
            <span>{formatDateTime(entry.createdAt, tag)}</span>
          </div>
        </div>
        <p className="mt-1.5 text-sm text-foreground">{snippet(entry.content, MAX_SNIPPET_CHARS)}</p>
        {filePath && (
          <p className="mt-1 text-xs text-muted-foreground">
            — {filePath}
            {heading ? ` § ${heading}` : ""}
          </p>
        )}
      </Link>
      <div className="mt-1.5 flex gap-1">
        <Button
          variant="ghost"
          size="xs"
          render={<Link to="/entries/$id/edit" params={{ id: entry.id }} />}
        >
          <Pencil data-icon="inline-start" />
          {t("common.edit")}
        </Button>
        <DeleteEntryDialog
          id={entry.id}
          trigger={
            <Button variant="ghost" size="xs" className="text-destructive">
              <Trash2 data-icon="inline-start" />
              {t("common.delete")}
            </Button>
          }
        />
      </div>
    </div>
  );
}
