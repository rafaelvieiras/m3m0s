// Context injection preview (D29) — GET /api/v1/preview/injection.
// Shows, for a real domain already present in the database, exactly the text
// that each injection mechanism produces TODAY (no fictional mechanisms):
//   - hook: the Claude Code SessionStart hook (D14/D17) — the only automatic one.
//   - mcp: the exact result of the memory_recent MCP tool — what a generic
//     MCP client (e.g. opencode) would see if it called the tool by hand.
//   - manual: raw apitypes.BuildContextBlock, to paste into a prompt of a
//     tool with neither hook nor MCP (scripts, cron).
// A step before deciding to implement new hooks/integrations — not the
// injection feature itself.
import { useState } from "react";
import { Check, Copy, Loader2 } from "lucide-react";
import { Route } from "@/routes/preview";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { usePreviewInjection } from "@/hooks/useMemoryApi";
import { useT, type TKey } from "@/i18n";
import type { PreviewInjectionMode } from "@/lib/schemas";

const MODE_LABEL: Record<PreviewInjectionMode, TKey> = {
  hook: "preview.mode.hook",
  mcp: "preview.mode.mcp",
  manual: "preview.mode.manual",
};

function CopyButton({ text }: { text: string }) {
  const { t } = useT();
  const [copied, setCopied] = useState(false);

  async function handleCopy() {
    await navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  }

  return (
    <Button size="sm" variant="outline" onClick={handleCopy}>
      {copied ? <Check data-icon="inline-start" /> : <Copy data-icon="inline-start" />}
      {copied ? t("preview.copied") : t("preview.copy")}
    </Button>
  );
}

export default function PreviewPage() {
  const { t } = useT();
  const search = Route.useSearch();
  const navigate = Route.useNavigate();

  const [domainInput, setDomainInput] = useState(search.domain);

  function applyDomain() {
    navigate({ search: { domain: domainInput, mode: search.mode } });
  }

  function changeMode(mode: PreviewInjectionMode) {
    navigate({ search: { domain: search.domain, mode } });
  }

  const { data, isLoading, isFetching, isError, error } = usePreviewInjection({
    domain: search.domain || undefined,
    mode: search.mode,
  });

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <PageHeader title={t("preview.title")} subtitle={t("preview.description")} help="preview" term="preview" />
        {isFetching && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
      </div>

      <Card>
        <CardContent className="flex flex-col gap-4 pt-6">
          <Field>
            <FieldLabel htmlFor="preview-domain">{t("preview.domain")}</FieldLabel>
            <div className="flex gap-2">
              <Input
                id="preview-domain"
                placeholder={t("preview.domainPlaceholder")}
                value={domainInput}
                onChange={(e) => setDomainInput(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && applyDomain()}
              />
              <Button variant="secondary" onClick={applyDomain}>
                {t("preview.apply")}
              </Button>
            </div>
          </Field>

          <Tabs value={search.mode} onValueChange={(value) => changeMode(value as PreviewInjectionMode)}>
            <TabsList>
              {(Object.keys(MODE_LABEL) as PreviewInjectionMode[]).map((mode) => (
                <TabsTrigger key={mode} value={mode}>
                  {t(MODE_LABEL[mode])}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
        </CardContent>
      </Card>

      {isLoading && (
        <div className="flex flex-col gap-2">
          <Skeleton className="h-6 w-2/3" />
          <Skeleton className="h-40 w-full" />
        </div>
      )}

      {isError && <p className="text-sm text-destructive">{t("preview.loadFailed", { message: error.message })}</p>}

      {data && (
        <Card>
          <CardHeader className="flex-row items-start justify-between gap-4 space-y-0">
            <div>
              <CardTitle className="text-sm">{t(MODE_LABEL[data.mode])}</CardTitle>
              <CardDescription>{data.mechanism}</CardDescription>
            </div>
            <CopyButton text={data.raw} />
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <div className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
              <Badge variant="secondary">{data.domain || t("preview.allDomains")}</Badge>
              <Badge variant="outline">{t("preview.fetched", { count: data.entriesFetched })}</Badge>
              <Badge variant="outline">{t("preview.used", { count: data.entriesUsed })}</Badge>
            </div>

            {data.raw.trim().length === 0 || data.entriesUsed === 0 ? (
              <Empty className="border">
                <EmptyHeader>
                  <EmptyTitle>{t("preview.empty.title")}</EmptyTitle>
                  <EmptyDescription>
                    {t("preview.empty.description")}
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : (
              <pre className="max-h-[32rem] overflow-auto rounded-lg border bg-muted/40 p-3 font-mono text-xs whitespace-pre-wrap">
                {data.raw}
              </pre>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  );
}
