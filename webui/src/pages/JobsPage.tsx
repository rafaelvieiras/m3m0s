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
import { PageHeader } from "@/components/layout/PageHeader";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
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
import { hasKey, useT, type TKey } from "@/i18n";
import { formatDateTime, formatInterval } from "@/lib/format";
import type { JobStatus } from "@/lib/schemas";

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

// jobTitle shows a friendly name for the built-in jobs and falls back to the
// raw name for any job registered in the future.
function jobTitle(name: string, t: (k: TKey) => string): string {
  const key = `jobs.name.${name}`;
  return hasKey(key) ? t(key) : name;
}

// bestUnitFor picks the largest unit that divides totalSeconds evenly —
// purely cosmetic, so "3600 seconds" is not shown when "1 hour" is more
// readable; the API always speaks in whole seconds underneath.
function bestUnitFor(totalSeconds: number): IntervalUnit {
  if (totalSeconds > 0 && totalSeconds % UNIT_FACTOR.hours === 0) return "hours";
  if (totalSeconds > 0 && totalSeconds % UNIT_FACTOR.minutes === 0) return "minutes";
  return "seconds";
}

// jobEffect explains what running the job does (and what it may cost); the
// built-in jobs have their own text, any future job gets a generic one.
function jobEffect(name: string, t: (k: TKey) => string): string {
  const key = `jobs.effect.${name}`;
  return hasKey(key) ? t(key) : t("jobs.effect.generic");
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
  const { t, tag } = useT();
  // Local state initialized once (lazy useState) from the job's current
  // value — it does not re-initialize on every polling refetch (useJobs,
  // 5s refetchInterval), otherwise a user editing the field would have their
  // typing overwritten by the next poll mid-edit.
  const [unit, setUnit] = useState<IntervalUnit>(() => bestUnitFor(job.intervalSeconds));
  // An interval of 0 means "manual only": the field starts empty instead of
  // showing a 0 that looks like an error.
  const [amount, setAmount] = useState<string>(() =>
    job.intervalSeconds > 0 ? String(job.intervalSeconds / UNIT_FACTOR[bestUnitFor(job.intervalSeconds)]) : "",
  );
  const setInterval = useSetJobInterval();
  const [confirmSeconds, setConfirmSeconds] = useState<number | null>(null);

  const parsedAmount = Number(amount);
  const canSave = amount.trim() !== "" && Number.isFinite(parsedAmount) && parsedAmount > 0;
  const showInvalid = amount.trim() !== "" && !canSave;

  // Saving asks for confirmation first: the change takes effect right away
  // and the next run is recalculated from now.
  function handleSave() {
    if (!canSave) return;
    setConfirmSeconds(Math.round(parsedAmount * UNIT_FACTOR[unit]));
  }

  return (
    <div className="flex flex-wrap items-end gap-2">
      <div className="flex flex-col gap-1">
        <span className="text-xs text-muted-foreground">
          {job.intervalSeconds > 0 ? t("jobs.interval.label") : t("jobs.interval.manualHint")}
        </span>
        <div className="flex items-center gap-1.5">
          <Input
            type="number"
            min={1}
            step={1}
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            className="w-20"
            aria-invalid={showInvalid || undefined}
            aria-label={t("jobs.interval.label")}
            placeholder=""
          />
          <Select value={unit} onValueChange={(value) => setUnit(value as IntervalUnit)}>
            <SelectTrigger size="sm" className="w-32">
              <SelectValue>{(value: IntervalUnit) => (value ? t(UNIT_LABEL[value]) : null)}</SelectValue>
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
      {showInvalid ? <p className="w-full text-xs text-destructive">{t("jobs.interval.invalid")}</p> : null}
      {setInterval.isError ? (
        <p className="w-full text-xs text-destructive">{setInterval.error.message}</p>
      ) : null}

      <AlertDialog open={confirmSeconds !== null} onOpenChange={(o) => (!o ? setConfirmSeconds(null) : undefined)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("jobs.confirmInterval.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {confirmSeconds !== null
                ? t("jobs.confirmInterval.description", {
                    job: jobTitle(job.name, t),
                    from: job.intervalSeconds > 0 ? formatInterval(job.intervalSeconds, tag) : t("jobs.interval.manual"),
                    to: formatInterval(confirmSeconds, tag),
                  })
                : null}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <p className="text-sm text-muted-foreground">{jobEffect(job.name, t)}</p>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("jobs.confirm.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (confirmSeconds !== null) setInterval.mutate({ name: job.name, seconds: confirmSeconds });
                setConfirmSeconds(null);
              }}
            >
              {t("jobs.confirmInterval.confirm")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function JobCard({ job }: { job: JobStatus }) {
  const { t, tag } = useT();
  const triggerJob = useTriggerJob();
  const isTriggeringThis = triggerJob.isPending && triggerJob.variables === job.name;
  const [confirmRun, setConfirmRun] = useState(false);

  return (
    <Card>
      <CardHeader className="flex-row items-start justify-between gap-4 space-y-0">
        <div>
          <CardTitle className="text-base">{jobTitle(job.name, t)}</CardTitle>
          <CardDescription>
            {job.intervalSeconds > 0
              ? t("jobs.card.schedule", { interval: formatInterval(job.intervalSeconds, tag), next: formatDateTime(job.nextRunAt, tag) })
              : t("jobs.card.manual")}
          </CardDescription>
        </div>
        <div className="flex items-center gap-2">
          <StatusBadge running={job.running} />
          <Button
            size="sm"
            disabled={job.running || isTriggeringThis}
            onClick={() => setConfirmRun(true)}
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

        <AlertDialog open={confirmRun} onOpenChange={setConfirmRun}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>{t("jobs.confirmRun.title", { job: jobTitle(job.name, t) })}</AlertDialogTitle>
              <AlertDialogDescription>{jobEffect(job.name, t)}</AlertDialogDescription>
            </AlertDialogHeader>
            <p className="text-sm text-muted-foreground">
              {job.lastRunAt
                ? t("jobs.confirmRun.lastDuration", { duration: formatDurationMs(job.lastDurationMs ?? 0) })
                : t("jobs.confirmRun.noDuration")}
            </p>
            <AlertDialogFooter>
              <AlertDialogCancel>{t("jobs.confirm.cancel")}</AlertDialogCancel>
              <AlertDialogAction
                onClick={() => {
                  setConfirmRun(false);
                  triggerJob.mutate(job.name);
                }}
              >
                {t("jobs.confirmRun.confirm")}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>

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
      <PageHeader title={t("jobs.page.title")} subtitle={t("jobs.page.description")} help="jobs" term="jobs" />

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
