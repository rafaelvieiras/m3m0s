// In-process jobs dashboard (internal/jobs, D16/D27) — GET /api/v1/jobs
// lists ALL jobs registered in the Scheduler; this page iterates that list
// generically (no hardcoded job names), so "forget-sweep" and
// "retroactive-extraction" show up for free today, and any new job
// registered in the future shows up without changing this file. Each card
// shows status/last run/history (docs/api-contract.md, section Jobs) and
// allows triggering a manual run or reconfiguring the interval without
// restarting the server (D27, docs/decisions/).
import { useState } from "react";
import { CheckCircle2, Loader2, Play, Save, XCircle } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useJobs, useSetJobInterval, useTriggerJob } from "@/hooks/useJobsApi";
import { useT, type TKey } from "@/i18n";
import type { JobStatus } from "@/lib/schemas";

function formatDateTime(iso: string, tag: string): string {
  return new Intl.DateTimeFormat(tag, { dateStyle: "short", timeStyle: "medium" }).format(new Date(iso));
}

function formatDurationMs(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  const seconds = ms / 1000;
  if (seconds < 60) return `${seconds.toFixed(seconds < 10 ? 1 : 0)}s`;
  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = Math.round(seconds % 60);
  return `${minutes}min ${remainingSeconds}s`;
}

type IntervalUnit = "seconds" | "minutes" | "hours";

const UNIT_FACTOR: Record<IntervalUnit, number> = {
  seconds: 1,
  minutes: 60,
  hours: 3600,
};

const UNIT_LABEL: Record<IntervalUnit, TKey> = {
  seconds: "jobs.unit.seconds",
  minutes: "jobs.unit.minutes",
  hours: "jobs.unit.hours",
};

// bestUnitFor picks the largest unit that divides totalSeconds evenly —
// purely cosmetic, so "3600 seconds" is not shown when "1 hour" is more
// readable; the API always speaks in whole seconds underneath.
function bestUnitFor(totalSeconds: number): IntervalUnit {
  if (totalSeconds > 0 && totalSeconds % UNIT_FACTOR.hours === 0) return "hours";
  if (totalSeconds > 0 && totalSeconds % UNIT_FACTOR.minutes === 0) return "minutes";
  return "seconds";
}

function StatusBadge({ running }: { running: boolean }) {
  const { t } = useT();
  return running ? (
    <Badge variant="secondary">
      <Loader2 data-icon="inline-start" className="animate-spin" />
      {t("jobs.status.running")}
    </Badge>
  ) : (
    <Badge variant="outline">{t("jobs.status.idle")}</Badge>
  );
}

function LastRunSummary({ job }: { job: JobStatus }) {
  const { t, tag } = useT();
  if (!job.lastRunAt) {
    return <p className="text-sm text-muted-foreground">{t("jobs.lastRun.never")}</p>;
  }
  return (
    <div className="flex flex-col gap-1">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-sm">{formatDateTime(job.lastRunAt, tag)}</span>
        <span className="text-xs text-muted-foreground">
          ({formatDurationMs(job.lastDurationMs ?? 0)})
        </span>
        {job.lastSuccess ? (
          <Badge variant="outline" className="text-green-600 dark:text-green-400">
            <CheckCircle2 data-icon="inline-start" />
            {t("jobs.lastRun.success")}
          </Badge>
        ) : (
          <Badge variant="destructive">
            <XCircle data-icon="inline-start" />
            {t("jobs.lastRun.failed")}
          </Badge>
        )}
      </div>
      {job.lastSummary ? <p className="text-sm text-muted-foreground">{job.lastSummary}</p> : null}
      {job.lastError ? <p className="text-sm text-destructive">{job.lastError}</p> : null}
    </div>
  );
}

function HistoryTable({ job }: { job: JobStatus }) {
  const { t, tag } = useT();
  if (job.history.length === 0) {
    return <p className="text-sm text-muted-foreground">{t("jobs.history.empty")}</p>;
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t("jobs.history.started")}</TableHead>
          <TableHead>{t("jobs.history.duration")}</TableHead>
          <TableHead>{t("jobs.history.status")}</TableHead>
          <TableHead>{t("jobs.history.details")}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {job.history.map((run, i) => (
          // The history has no ID of its own — startedAt+index is stable
          // enough for a read-only list (most recent first, already sorted by
          // the backend, internal/jobs.Scheduler.Status).
          <TableRow key={`${run.startedAt}-${i}`}>
            <TableCell className="whitespace-nowrap">{formatDateTime(run.startedAt, tag)}</TableCell>
            <TableCell className="whitespace-nowrap">{formatDurationMs(run.durationMs)}</TableCell>
            <TableCell>
              {run.success ? (
                <Badge variant="outline" className="text-green-600 dark:text-green-400">
                  {t("jobs.history.ok")}
                </Badge>
              ) : (
                <Badge variant="destructive">{t("jobs.history.error")}</Badge>
              )}
            </TableCell>
            <TableCell className="max-w-xs truncate text-muted-foreground">
              {run.error || run.summary || "—"}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

function IntervalEditor({ job }: { job: JobStatus }) {
  const { t } = useT();
  // Local state initialized once (lazy useState) from the job's current
  // value — it does not re-initialize on every polling refetch (useJobs,
  // 5s refetchInterval), otherwise a user editing the field would have their
  // typing overwritten by the next poll mid-edit.
  const [unit, setUnit] = useState<IntervalUnit>(() => bestUnitFor(job.intervalSeconds));
  const [amount, setAmount] = useState<string>(() =>
    String(job.intervalSeconds / UNIT_FACTOR[bestUnitFor(job.intervalSeconds)]),
  );
  const setInterval = useSetJobInterval();

  const parsedAmount = Number(amount);
  const canSave = Number.isFinite(parsedAmount) && parsedAmount > 0;

  function handleSave() {
    if (!canSave) return;
    setInterval.mutate({ name: job.name, seconds: Math.round(parsedAmount * UNIT_FACTOR[unit]) });
  }

  return (
    <div className="flex flex-wrap items-end gap-2">
      <div className="flex flex-col gap-1">
        <span className="text-xs text-muted-foreground">{t("jobs.interval.label")}</span>
        <div className="flex items-center gap-1.5">
          <Input
            type="number"
            min={1}
            step={1}
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            className="w-20"
            aria-invalid={!canSave || undefined}
          />
          <Select value={unit} onValueChange={(value) => setUnit(value as IntervalUnit)}>
            <SelectTrigger size="sm" className="w-28">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {(Object.keys(UNIT_LABEL) as IntervalUnit[]).map((u) => (
                <SelectItem key={u} value={u}>
                  {t(UNIT_LABEL[u])}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>
      <Button
        size="sm"
        variant="secondary"
        disabled={!canSave || setInterval.isPending}
        onClick={handleSave}
      >
        {setInterval.isPending ? (
          <Loader2 data-icon="inline-start" className="animate-spin" />
        ) : (
          <Save data-icon="inline-start" />
        )}
        {t("jobs.interval.save")}
      </Button>
      {setInterval.isError ? (
        <p className="w-full text-xs text-destructive">{setInterval.error.message}</p>
      ) : null}
    </div>
  );
}

function JobCard({ job }: { job: JobStatus }) {
  const { t, tag } = useT();
  const triggerJob = useTriggerJob();
  const isTriggeringThis = triggerJob.isPending && triggerJob.variables === job.name;

  return (
    <Card>
      <CardHeader className="flex-row items-start justify-between gap-4 space-y-0">
        <div>
          <CardTitle className="font-mono text-base">{job.name}</CardTitle>
          <CardDescription>
            {t("jobs.card.schedule", { seconds: job.intervalSeconds, next: formatDateTime(job.nextRunAt, tag) })}
          </CardDescription>
        </div>
        <div className="flex items-center gap-2">
          <StatusBadge running={job.running} />
          <Button
            size="sm"
            disabled={job.running || isTriggeringThis}
            onClick={() => triggerJob.mutate(job.name)}
          >
            {isTriggeringThis ? (
              <Loader2 data-icon="inline-start" className="animate-spin" />
            ) : (
              <Play data-icon="inline-start" />
            )}
            {t("jobs.card.runNow")}
          </Button>
        </div>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {triggerJob.isError && triggerJob.variables === job.name ? (
          <p className="text-sm text-destructive">{triggerJob.error.message}</p>
        ) : null}

        <IntervalEditor job={job} />

        <Separator />

        <div>
          <h3 className="mb-1.5 text-sm font-medium">{t("jobs.card.lastRun")}</h3>
          <LastRunSummary job={job} />
        </div>

        <Separator />

        <div>
          <h3 className="mb-1.5 text-sm font-medium">{t("jobs.card.recentHistory")}</h3>
          <HistoryTable job={job} />
        </div>
      </CardContent>
    </Card>
  );
}

export default function JobsPage() {
  const { t } = useT();
  const { data, isLoading, isError, error } = useJobs();

  if (isLoading) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-40" />
        <Skeleton className="h-40" />
      </div>
    );
  }

  if (isError || !data) {
    return (
      <p className="text-sm text-destructive">
        {t("jobs.page.loadFailed", { message: error?.message ?? t("jobs.page.unexpected") })}
      </p>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h1 className="text-lg font-semibold">{t("jobs.page.title")}</h1>
        <p className="text-sm text-muted-foreground">
          {t("jobs.page.description")}
        </p>
      </div>

      {data.jobs.length === 0 ? (
        <Card>
          <CardContent className="pt-6">
            <p className="text-sm text-muted-foreground">{t("jobs.page.empty")}</p>
          </CardContent>
        </Card>
      ) : (
        <div className="flex flex-col gap-4">
          {data.jobs.map((job) => (
            <JobCard key={job.name} job={job} />
          ))}
        </div>
      )}
    </div>
  );
}
