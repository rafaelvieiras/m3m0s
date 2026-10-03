// Stats dashboard (GET /stats) — count per domainCategory (pure CSS bars, no
// heavy chart lib) + embedding backend/model + fact extractor status
// (D13/api-contract.md, `extraction` field) + operational telemetry
// (`telemetry` field): process memory/disk, issued API keys and the token
// savings that fact extraction provides.
import { Clock, Coins, Database, KeyRound, MemoryStick, PiggyBank, Sparkles, SparklesIcon } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { useStats } from "@/hooks/useMemoryApi";
import type { StatsResponse } from "@/lib/schemas";

const numberFormatter = new Intl.NumberFormat("en-US");

function formatMB(mb: number): string {
  if (mb >= 1024) return `${numberFormatter.format(Math.round((mb / 1024) * 100) / 100)} GB`;
  return `${numberFormatter.format(mb)} MB`;
}

function formatUptime(seconds: number): string {
  const totalMinutes = Math.floor(seconds / 60);
  const days = Math.floor(totalMinutes / (60 * 24));
  const hours = Math.floor((totalMinutes % (60 * 24)) / 60);
  const minutes = totalMinutes % 60;
  if (days > 0) return `${days}d ${hours}h`;
  if (hours > 0) return `${hours}h ${minutes}min`;
  return `${minutes}min`;
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
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Telemetry</CardTitle>
        <CardDescription>Process/database footprint and the token savings from fact extraction.</CardDescription>
      </CardHeader>
      <CardContent>
        <div className="grid gap-4 sm:grid-cols-3">
          <TelemetryTile
            icon={<KeyRound className="size-5" />}
            label="API keys"
            value={numberFormatter.format(telemetry.apiKeyCount)}
          />
          <TelemetryTile
            icon={<MemoryStick className="size-5" />}
            label="Process memory"
            value={formatMB(telemetry.memoryUsageMB)}
          />
          <TelemetryTile
            icon={<Database className="size-5" />}
            label="Database size"
            value={formatMB(telemetry.databaseSizeMB)}
          />
          <TelemetryTile
            icon={<Clock className="size-5" />}
            label="Uptime"
            value={formatUptime(telemetry.uptimeSeconds)}
          />
          <TelemetryTile
            icon={<Coins className="size-5" />}
            label="Tokens used (extraction)"
            value={numberFormatter.format(telemetry.extractionTokensUsed)}
            hint="prompt + completion, since the server started"
          />
          <TelemetryTile
            icon={<PiggyBank className="size-5" />}
            label="Tokens saved"
            value={numberFormatter.format(telemetry.tokensSavedByCompaction)}
            hint="estimate — how much extraction shrank the content"
          />
        </div>
      </CardContent>
    </Card>
  );
}

export default function StatsPage() {
  const { data, isLoading, isError, error } = useStats();

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
    return <p className="text-sm text-destructive">Failed to load stats: {error?.message}</p>;
  }

  const entries = Object.entries(data.countByDomainCategory).sort((a, b) => b[1] - a[1]);
  const max = Math.max(1, ...entries.map(([, n]) => n));

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-lg font-semibold">Stats</h1>

      <div className="grid gap-4 sm:grid-cols-2">
        <Card>
          <CardHeader>
            <CardDescription>Total memories</CardDescription>
            <CardTitle className="text-3xl">{data.count}</CardTitle>
          </CardHeader>
        </Card>

        <Card>
          <CardHeader className="flex-row items-center justify-between space-y-0">
            <div>
              <CardDescription>Fact extraction (Phase 4/D12)</CardDescription>
              <CardTitle className="text-base">
                {data.extraction.enabled ? "On" : "Off"}
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
              <Badge variant="secondary">model: {data.extraction.model}</Badge>
            ) : (
              <p className="text-sm text-muted-foreground">
                `memory_add` stores the raw content as 1 entry (no extraction).
              </p>
            )}
          </CardContent>
        </Card>

        <Card className="sm:col-span-2">
          <CardHeader>
            <CardDescription>Backend</CardDescription>
            <CardTitle className="text-base">{data.backend}</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm text-muted-foreground">Embedding model: {data.model}</p>
          </CardContent>
        </Card>
      </div>

      <TelemetrySection telemetry={data.telemetry} />

      <Card>
        <CardHeader>
          <CardTitle className="text-base">By domainCategory</CardTitle>
          <CardDescription>Field derived from the `domain` prefix (D9/memory-schema.md).</CardDescription>
        </CardHeader>
        <CardContent>
          {entries.length === 0 ? (
            <p className="text-sm text-muted-foreground">No memories yet.</p>
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
                    {count}
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
