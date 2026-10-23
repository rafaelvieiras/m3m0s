// Stats dashboard (GET /stats) — count per domainCategory (pure CSS bars, no
// heavy chart lib) + embedding backend/model + fact extractor status
// (D13/api-contract.md, `extraction` field) + operational telemetry
// (`telemetry` field): process memory/disk, issued API keys and the token
// savings that fact extraction provides.
import { Clock, Coins, Database, KeyRound, MemoryStick, PiggyBank, Sparkles, SparklesIcon } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { useT } from "@/i18n";
import { useStats } from "@/hooks/useMemoryApi";
import { useEmbeddingConfig } from "@/hooks/useEmbeddingApi";
import type { StatsResponse } from "@/lib/schemas";

function formatMB(mb: number, tag: string): string {
  const nf = new Intl.NumberFormat(tag);
  if (mb >= 1024) return `${nf.format(Math.round((mb / 1024) * 100) / 100)} GB`;
  return `${nf.format(mb)} MB`;
}

function formatUptime(seconds: number, t: (k: "stats.unit.d" | "stats.unit.h" | "stats.unit.min") => string): string {
  const totalMinutes = Math.floor(seconds / 60);
  const days = Math.floor(totalMinutes / (60 * 24));
  const hours = Math.floor((totalMinutes % (60 * 24)) / 60);
  const minutes = totalMinutes % 60;
  if (days > 0) return `${days}${t("stats.unit.d")} ${hours}${t("stats.unit.h")}`;
  if (hours > 0) return `${hours}${t("stats.unit.h")} ${minutes}${t("stats.unit.min")}`;
  return `${minutes}${t("stats.unit.min")}`;
}

type TelemetryTileProps = {
  icon: React.ReactNode;
  label: string;
  value: string;
  hint?: string;
};

function TelemetryTile({ icon, label, value, hint }: TelemetryTileProps) {
  return (
    <div className="flex items-start gap-3">
      <div className="mt-0.5 text-muted-foreground">{icon}</div>
      <div className="flex flex-col">
        <span className="text-xs text-muted-foreground">{label}</span>
        <span className="text-lg font-semibold leading-tight">{value}</span>
        {hint ? <span className="text-xs text-muted-foreground">{hint}</span> : null}
      </div>
    </div>
  );
}

function TelemetrySection({ telemetry }: { telemetry: StatsResponse["telemetry"] }) {
  const { t, tag } = useT();
  const numberFormatter = new Intl.NumberFormat(tag);
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t("stats.telemetry.title")}</CardTitle>
        <CardDescription>{t("stats.telemetry.desc")}</CardDescription>
      </CardHeader>
      <CardContent>
        <div className="grid gap-4 sm:grid-cols-3">
          <TelemetryTile
            icon={<KeyRound className="size-5" />}
            label={t("stats.telemetry.apiKeys")}
            value={numberFormatter.format(telemetry.apiKeyCount)}
          />
          <TelemetryTile
            icon={<MemoryStick className="size-5" />}
            label={t("stats.telemetry.processMemory")}
            value={formatMB(telemetry.memoryUsageMB, tag)}
          />
          <TelemetryTile
            icon={<Database className="size-5" />}
            label={t("stats.telemetry.dbSize")}
            value={formatMB(telemetry.databaseSizeMB, tag)}
          />
          <TelemetryTile
            icon={<Clock className="size-5" />}
            label={t("stats.telemetry.uptime")}
            value={formatUptime(telemetry.uptimeSeconds, t)}
          />
          <TelemetryTile
            icon={<Coins className="size-5" />}
            label={t("stats.telemetry.tokensUsed")}
            value={numberFormatter.format(telemetry.extractionTokensUsed)}
            hint={t("stats.telemetry.tokensUsedHint")}
          />
          <TelemetryTile
            icon={<PiggyBank className="size-5" />}
            label={t("stats.telemetry.tokensSaved")}
            value={numberFormatter.format(telemetry.tokensSavedByCompaction)}
            hint={t("stats.telemetry.tokensSavedHint")}
          />
        </div>
      </CardContent>
    </Card>
  );
}

export default function StatsPage() {
  const { t, tag } = useT();
  const { data, isLoading, isError, error } = useStats();
  const embedding = useEmbeddingConfig();
  const semantic = embedding.data?.active === true;

  if (isLoading) {
    return (
      <div className="grid gap-4 sm:grid-cols-2">
        <Skeleton className="h-32" />
        <Skeleton className="h-32" />
        <Skeleton className="h-48 sm:col-span-2" />
      </div>
    );
  }

  if (isError || !data) {
    return <p className="text-sm text-destructive">{t("stats.loadError", { message: error?.message ?? "" })}</p>;
  }

  const entries = Object.entries(data.countByDomainCategory).sort((a, b) => b[1] - a[1]);
  const max = Math.max(1, ...entries.map(([, n]) => n));

  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={t("stats.title")} help="stats" />

      <div className="grid gap-4 sm:grid-cols-2">
        <Card>
          <CardHeader>
            <CardDescription>{t("stats.total")}</CardDescription>
            <CardTitle className="text-3xl">{new Intl.NumberFormat(tag).format(data.count)}</CardTitle>
          </CardHeader>
        </Card>

        <Card>
          <CardHeader className="flex-row items-center justify-between space-y-0">
            <div>
              <CardDescription>{t("stats.extraction")}</CardDescription>
              <CardTitle className="text-base">
                {data.extraction.enabled ? t("stats.on") : t("stats.off")}
              </CardTitle>
            </div>
            {data.extraction.enabled ? (
              <Sparkles className="size-6 text-primary" />
            ) : (
              <SparklesIcon className="size-6 text-muted-foreground" />
            )}
          </CardHeader>
          <CardContent>
            {data.extraction.enabled ? (
              <Badge variant="secondary">{t("stats.modelBadge", { model: data.extraction.model ?? "" })}</Badge>
            ) : (
              <p className="text-sm text-muted-foreground">
                {t("stats.extractionOff")}
              </p>
            )}
          </CardContent>
        </Card>

        <Card className="sm:col-span-2">
          <CardHeader>
            <CardDescription>{t("stats.backend")}</CardDescription>
            <CardTitle className="text-base">
              {semantic ? t("stats.backendSemantic") : t("stats.backendKeyword")}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm text-muted-foreground">
              {semantic ? t("stats.embeddingModel", { model: data.model }) : t("stats.embeddingsOff")}
            </p>
          </CardContent>
        </Card>
      </div>

      <TelemetrySection telemetry={data.telemetry} />

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("stats.byCategory")}</CardTitle>
          <CardDescription>{t("stats.byCategoryDesc")}</CardDescription>
        </CardHeader>
        <CardContent>
          {entries.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t("stats.empty")}</p>
          ) : (
            <div className="flex flex-col gap-2">
              {entries.map(([category, count]) => (
                <div key={category} className="flex items-center gap-3">
                  <span className="w-32 shrink-0 truncate text-sm font-medium">{category}</span>
                  <div className="h-2 flex-1 overflow-hidden rounded-full bg-muted">
                    <div
                      className="h-full rounded-full bg-primary"
                      style={{ width: `${(count / max) * 100}%` }}
                    />
                  </div>
                  <span className="w-10 shrink-0 text-right text-sm text-muted-foreground">
                    {new Intl.NumberFormat(tag).format(count)}
                  </span>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
