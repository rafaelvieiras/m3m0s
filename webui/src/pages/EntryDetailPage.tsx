// Detail view: full content, all metadata, and — when the entry came from
// extraction (D13, item 3) — the "Extracted from: ..." section with the
// original text (metadata.source_content).
import { Route } from "@/routes/entry-detail";
import { useT } from "@/i18n";
import { Link } from "@tanstack/react-router";
import { ArrowLeft, Pencil } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Separator } from "@/components/ui/separator";
import { Markdown } from "@/components/ui/markdown";
import { DeleteEntryDialog } from "@/components/memory/DeleteEntryDialog";
import { useEntry } from "@/hooks/useMemoryApi";

export default function EntryDetailPage() {
  const { t, tag } = useT();
  const { id } = Route.useParams();
  const navigate = Route.useNavigate();
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
  const otherMetadata = Object.entries(entry.metadata).filter(([k]) => k !== "source_content");

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
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
          <div className="flex flex-wrap items-center gap-1.5">
            <Badge variant="secondary">{entry.domain}</Badge>
            <Badge variant="outline">{entry.domainCategory}</Badge>
            <Badge variant="outline">{entry.source}</Badge>
            {entry.kind && (
              <Badge variant="outline" title={entry.kindSource ? t("memory.detail.kindSetBy", { source: entry.kindSource }) : t("memory.detail.neverClassified")}>
                {entry.kind}
              </Badge>
            )}
            {entry.agent && <Badge variant="outline">@{entry.agent}</Badge>}
            {entry.project && <Badge variant="outline">project:{entry.project}</Badge>}
            {entry.model && <Badge variant="outline">{entry.model}</Badge>}
            {entry.tags.map((tag) => (
              <Badge key={tag} variant="outline" className="text-muted-foreground">
                #{tag}
              </Badge>
            ))}
          </div>
          <CardTitle className="mt-2 font-mono text-xs font-normal text-muted-foreground">
            {entry.id}
          </CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <p className="whitespace-pre-wrap text-sm">{entry.content}</p>

          <div className="flex gap-4 text-xs text-muted-foreground">
            <span>{t("memory.detail.createdAt", { date: new Date(entry.createdAt).toLocaleString(tag) })}</span>
            <span>{t("memory.detail.updatedAt", { date: new Date(entry.updatedAt).toLocaleString(tag) })}</span>
          </div>

          {sourceContent && (
            <>
              <Separator />
              <div>
                <h2 className="mb-1 text-sm font-medium">{t("memory.detail.extractedFrom")}</h2>
                <div className="rounded-md bg-muted p-3 text-muted-foreground">
                  <Markdown content={sourceContent} />
                </div>
              </div>
            </>
          )}

          {otherMetadata.length > 0 && (
            <>
              <Separator />
              <div>
                <h2 className="mb-1 text-sm font-medium">{t("memory.detail.metadata")}</h2>
                <dl className="grid grid-cols-[max-content_1fr] gap-x-3 gap-y-1 text-sm">
                  {otherMetadata.map(([key, value]) => (
                    <div key={key} className="contents">
                      <dt className="font-mono text-muted-foreground">{key}</dt>
                      <dd className="break-all">{value}</dd>
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
