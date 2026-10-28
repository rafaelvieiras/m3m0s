// Detail view: full content, friendly details, and — when the entry came from
// extraction (D13, item 3) — the "Extracted from: ..." section with the
// original text (metadata.source_content).
import { Route } from "@/routes/entry-detail";
import { hasKey, useT, type TKey } from "@/i18n";
import { Link } from "@tanstack/react-router";
import { ArrowLeft, Pencil } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Separator } from "@/components/ui/separator";
import { Markdown } from "@/components/ui/markdown";
import { DeleteEntryDialog } from "@/components/memory/DeleteEntryDialog";
import { KindBadge } from "@/components/memory/KindBadge";
import { useEntry } from "@/hooks/useMemoryApi";
import { formatDateTime } from "@/lib/format";
import { kindSourceLabel } from "@/lib/kinds";
import { useAdvancedMode } from "@/lib/mode";

const ISO_DATE = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}/;

// Metadata keys that are already shown elsewhere on the page.
const HIDDEN_METADATA = new Set(["source_content", "kind"]);

/** "access_count" -> "Times used"; unknown keys become "Some key" instead of snake_case. */
function metadataLabel(key: string, t: (k: TKey) => string): string {
  const known = `memory.meta.${key}`;
  if (hasKey(known)) return t(known);
  const spaced = key.replace(/[_-]+/g, " ").trim();
  return spaced.charAt(0).toUpperCase() + spaced.slice(1);
}

function metadataValue(key: string, value: string, t: (k: TKey) => string, tag: string): string {
  if (ISO_DATE.test(value)) return formatDateTime(value, tag);
  if (key === "kind_source") return kindSourceLabel(value);
  if (key === "pinned") return value === "true" ? t("memory.meta.yes") : t("memory.meta.no");
  return value;
}

export default function EntryDetailPage() {
  const { t, tag } = useT();
  const { id } = Route.useParams();
  const navigate = Route.useNavigate();
  const [advanced] = useAdvancedMode();
  const { data: entry, isError, error } = useEntry(id);

  if (isError || !entry) {
    return (
      <Empty className="border">
        <EmptyHeader>
          <EmptyTitle>{t("memory.notFound.title")}</EmptyTitle>
          <EmptyDescription>
            {error?.message ?? t("memory.notFound.hint")}
          </EmptyDescription>
        </EmptyHeader>
        <Button variant="outline" render={<Link to="/search" />}>
          <ArrowLeft data-icon="inline-start" />
          {t("memory.notFound.back")}
        </Button>
      </Empty>
    );
  }

  const sourceContent = entry.metadata.source_content;
  const otherMetadata = Object.entries(entry.metadata).filter(([k]) => !HIDDEN_METADATA.has(k));

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Button variant="ghost" size="sm" render={<Link to="/search" />}>
          <ArrowLeft data-icon="inline-start" />
          {t("memory.detail.back")}
        </Button>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" render={<Link to="/entries/$id/edit" params={{ id: entry.id }} />}>
            <Pencil data-icon="inline-start" />
            {t("memory.detail.edit")}
          </Button>
          <DeleteEntryDialog id={entry.id} onDeleted={() => navigate({ to: "/search" })} />
        </div>
      </div>

      <Card>
        <CardHeader>
          <h1 className="sr-only">{t("memory.detail.title")}</h1>
          <div className="flex flex-wrap items-center gap-1.5">
            <KindBadge kind={entry.kind} kindSource={entry.kindSource} />
            {entry.project && <Badge variant="secondary">{t("memory.detail.project", { project: entry.project })}</Badge>}
            <Badge variant="outline">{entry.domain}</Badge>
            {advanced && <Badge variant="outline">{entry.source}</Badge>}
            {advanced && entry.agent && <Badge variant="outline">@{entry.agent}</Badge>}
            {advanced && entry.model && <Badge variant="outline">{entry.model}</Badge>}
            {entry.tags.map((tagName) => (
              <Badge key={tagName} variant="outline" className="text-muted-foreground">
                #{tagName}
              </Badge>
            ))}
          </div>
          {advanced && (
            <CardTitle className="mt-2 font-mono text-xs font-normal break-all text-muted-foreground">
              {entry.id}
            </CardTitle>
          )}
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <div className="text-sm [overflow-wrap:anywhere]">
            <Markdown content={entry.content} />
          </div>

          <div className="flex flex-col gap-0.5 text-xs text-muted-foreground sm:flex-row sm:gap-4">
            <span>{t("memory.detail.createdAt", { date: formatDateTime(entry.createdAt, tag) })}</span>
            <span>{t("memory.detail.updatedAt", { date: formatDateTime(entry.updatedAt, tag) })}</span>
          </div>

          {sourceContent && (
            <>
              <Separator />
              <div>
                <h2 className="mb-1 text-sm font-medium">{t("memory.detail.extractedFrom")}</h2>
                <div className="rounded-md bg-muted p-3 text-muted-foreground [overflow-wrap:anywhere]">
                  <Markdown content={sourceContent} />
                </div>
              </div>
            </>
          )}

          {advanced && otherMetadata.length > 0 && (
            <>
              <Separator />
              <div>
                <h2 className="mb-1 text-sm font-medium">{t("memory.detail.metadata")}</h2>
                <dl className="grid grid-cols-[max-content_1fr] gap-x-3 gap-y-1 text-sm">
                  {otherMetadata.map(([key, value]) => (
                    <div key={key} className="contents">
                      <dt className="text-muted-foreground">{metadataLabel(key, t)}</dt>
                      <dd className="[overflow-wrap:anywhere]">{metadataValue(key, value, t, tag)}</dd>
                    </div>
                  ))}
                </dl>
              </div>
            </>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
