// Memory health dashboard (D37). The top half is live and cheap (GET
// /insights/overview: totals, distributions, forgetting outlook). The bottom
// half is the hygiene analysis (GET /insights/hygiene): a read-only job that
// looks for duplicates and low-value memories and stores its last result, so
// the page shows when it was computed and never recomputes on its own.
// Charts are plain CSS bars: no chart library is needed for this.
import { AlertTriangle, CheckCircle2, Loader2, Play, Square } from "lucide-react";
import { Link } from "@tanstack/react-router";
import { toast } from "sonner";
import { PageHeader } from "@/components/layout/PageHeader";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useT } from "@/i18n";
import { ageBucketLabel, formatDateTime, formatDecimal, formatPercentValue } from "@/lib/format";
import { kindLabel, kindSourceLabel } from "@/lib/kinds";
import { useCancelHygiene, useHygiene, useInsightsOverview, useStartHygiene } from "@/hooks/useInsightsApi";
import {
  barWidths,
  domainInstance,
  formatCount,
  formatDuration,
  formatPercent,
  horizonLabel,
  memoryHealth,
  progressPercent,
  shadowDelta,
  tierLabel,
} from "@/lib/insights";
import type {
  CountItem,
  ExampleEntry,
  FilterOverview,
  ForgettingStats,
  HygieneReport,
  HygieneStatus,
  InsightsOverview,
  NoiseCriterion,
  RetentionShadow,
} from "@/lib/schemas";


function Tile({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="flex flex-col rounded-lg border p-3">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span className="text-2xl font-semibold leading-tight">{value}</span>
      {hint ? <span className="text-xs text-muted-foreground">{hint}</span> : null}
    </div>
  );
}

function BarList({ items, empty, label }: { items: CountItem[]; empty?: string; label?: (key: string) => string }) {
  const { t } = useT();
  if (items.length === 0) return <p className="text-sm text-muted-foreground">{empty ?? t("insights.noData")}</p>;
  const widths = barWidths(items);
  return (
    <ul className="flex flex-col gap-1.5">
      {items.map((item, i) => (
        <li key={item.key} className="grid grid-cols-[minmax(0,10rem)_1fr_auto] items-center gap-2 text-sm">
          <span className="truncate" title={label ? label(item.key) : item.key}>
            {label ? label(item.key) : item.key}
          </span>
          <div className="h-2 overflow-hidden rounded-full bg-muted">
            <div className="h-full rounded-full bg-primary" style={{ width: `${widths[i]}%` }} />
          </div>
          <span className="tabular-nums text-muted-foreground">{formatCount(item.count)}</span>
        </li>
      ))}
    </ul>
  );
}

// Days with soft-deletes as vertical bars (one per day, oldest first).
function DayBars({ items }: { items: CountItem[] }) {
  const { t } = useT();
  if (items.length === 0) return <p className="text-sm text-muted-foreground">{t("insights.softDeleted.empty")}</p>;
  const widths = barWidths(items);
  return (
    <div>
      <div className="flex h-24 items-end gap-0.5" role="img" aria-label={t("insights.softDeleted.aria")}>
        {items.map((item, i) => (
          <div
            key={item.key}
            className="flex-1 rounded-t bg-primary/80"
            style={{ height: `${widths[i]}%` }}
            title={`${item.key}: ${formatCount(item.count)}`}
          />
        ))}
      </div>
      <div className="mt-1 flex justify-between text-xs text-muted-foreground">
        <span>{items[0].key}</span>
        <span>{items[items.length - 1].key}</span>
      </div>
    </div>
  );
}

function Section({ title, description, children }: { title: string; description?: string; children: React.ReactNode }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{title}</CardTitle>
        {description ? <CardDescription>{description}</CardDescription> : null}
      </CardHeader>
      <CardContent>{children}</CardContent>
    </Card>
  );
}

// ── Health summary ──

// A plain verdict for people who do not read charts: all good, or up to three
// things worth doing, each linking to where it can be dealt with.
function HealthSummary({ ov }: { ov: InsightsOverview }) {
  const { t } = useT();
  const hygiene = useHygiene();
  // Wait for the last analysis so "run an analysis" is not shown by mistake.
  if (hygiene.isLoading) return <Skeleton className="h-20 w-full" />;
  const health = memoryHealth(ov, hygiene.data?.report);
  const ok = health.status === "ok";
  return (
    <Card id="health" className="scroll-mt-4">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          {ok ? (
            <CheckCircle2 className="size-5 text-emerald-600 dark:text-emerald-400" aria-hidden />
          ) : (
            <AlertTriangle className="size-5 text-amber-600 dark:text-amber-400" aria-hidden />
          )}
          {ok ? t("insights.health.ok") : t("insights.health.attention")}
        </CardTitle>
        <CardDescription>{ok ? t("insights.health.okDesc") : t("insights.health.attentionDesc")}</CardDescription>
      </CardHeader>
      {ok ? null : (
        <CardContent>
          <ul className="flex flex-col gap-2">
            {health.issues.map((issue) => (
              <li key={issue.id} className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1 text-sm">
                <span>{t(`insights.health.issue.${issue.id}`, { count: formatCount(issue.count) })}</span>
                {"route" in issue.to ? (
                  <Link to={issue.to.route} className="text-primary underline-offset-4 hover:underline">
                    {t(`insights.health.action.${issue.id}`)}
                  </Link>
                ) : (
                  <a href={`#${issue.to.anchor}`} className="text-primary underline-offset-4 hover:underline">
                    {t(`insights.health.action.${issue.id}`)}
                  </a>
                )}
              </li>
            ))}
          </ul>
        </CardContent>
      )}
    </Card>
  );
}

// ── Overview ──

function OverviewSection({ ov }: { ov: InsightsOverview }) {
  const { t, tag } = useT();
  const o = ov.totals;
  return (
    <>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <Tile
          label={t("insights.ov.active")}
          value={formatCount(o.active)}
          hint={t("insights.ov.activeHint", { total: formatCount(o.total) })}
        />
        <Tile
          label={t("insights.ov.softDeleted")}
          value={formatCount(o.soft_deleted)}
          hint={t("insights.ov.softDeletedHint")}
        />
        <Tile
          label={t("insights.neverAccessed")}
          value={formatPercentValue(o.never_accessed_pct, tag)}
          hint={t("insights.ov.neverAccessedHint", { count: formatCount(o.never_accessed), active: formatCount(o.active) })}
        />
        <Tile
          label={t("insights.pastValidUntil")}
          value={formatCount(o.expired)}
          hint={t("insights.ov.pastValidUntilHint", { count: formatCount(o.with_valid_until), pct: formatPercent(o.expired, o.active) })}
        />
        <Tile label={t("insights.ov.pinned")} value={formatCount(o.pinned)} hint={t("insights.ov.pinnedHint")} />
        <Tile
          label={t("insights.ov.extracted")}
          value={formatCount(o.extracted)}
          hint={t("insights.ov.extractedHint", { raw: formatCount(o.raw), pct: formatPercent(o.extracted, o.active) })}
        />
        <Tile
          label={t("insights.ov.avgLen")}
          value={t("insights.ov.chars", { n: Math.round(ov.text.avg_chars) })}
          hint={t("insights.ov.avgLenHint", { median: ov.text.median_chars, p90: ov.text.p90_chars, max: formatCount(ov.text.max_chars) })}
        />
        <Tile
          label={t("insights.ov.vectors")}
          value={formatCount(ov.vectors_indexed)}
          hint={t("insights.domainsCount", { count: ov.domain_count })}
        />
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <Section title={t("insights.ov.byAge")} description={t("insights.ov.byAgeDesc")}>
          <BarList items={ov.by_age} label={ageBucketLabel} />
        </Section>
        <Section title={t("insights.ov.bySource")} description={t("insights.ov.bySourceDesc")}>
          <BarList items={ov.by_source} />
        </Section>
        <Section
          title={t("insights.ov.byKind")}
          description={t("insights.ov.byKindDesc")}
        >
          <BarList items={ov.by_kind ?? []} label={kindLabel} />
        </Section>
        <Section title={t("insights.ov.byKindSource")} description={t("insights.ov.byKindSourceDesc")}>
          <BarList items={ov.by_kind_source ?? []} label={kindSourceLabel} />
        </Section>
        <Section title={t("insights.ov.byCategory")}>
          <BarList items={ov.by_category} />
        </Section>
        <Section title={t("insights.ov.perDay")} description={t("insights.ov.perDayDesc")}>
          <DayBars items={ov.soft_deleted_by_day} />
        </Section>
      </div>

      <Section title={t("insights.ov.topDomains")} description={t("insights.ov.topDomainsDesc", { count: ov.domain_count })}>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t("insights.ov.colDomain")}</TableHead>
              <TableHead className="text-right">{t("insights.ov.colMemories")}</TableHead>
              <TableHead className="text-right">{t("insights.neverAccessed")}</TableHead>
              <TableHead className="text-right">{t("insights.pastValidUntil")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {ov.top_domains.map((d) => (
              <TableRow key={d.domain}>
                <TableCell className="font-mono text-xs" title={d.domain}>
                  {d.domain}
                </TableCell>
                <TableCell className="text-right tabular-nums">{formatCount(d.count)}</TableCell>
                <TableCell className="text-right tabular-nums">{formatPercent(d.never_accessed, d.count)}</TableCell>
                <TableCell className="text-right tabular-nums">{formatCount(d.expired)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Section>
    </>
  );
}

function ForgettingSection({ f }: { f: ForgettingStats }) {
  const { t } = useT();
  return (
    <Section
      title={t("insights.fg.title")}
      description={t("insights.fg.desc", { model: f.model ?? t("insights.fg.legacy"), threshold: f.threshold })}
    >
      <div className="flex flex-col gap-4">
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <Tile label={t("insights.fg.cold")} value={formatCount(f.cold_now)} hint={t("insights.fg.coldHint")} />
          <Tile
            label={t("insights.fg.waiting")}
            value={formatCount(f.soft_deleted)}
            hint={t("insights.fg.waitingHint", { count: formatCount(f.hard_delete_due), days: f.grace_days })}
          />
          <Tile label={t("insights.fg.eligible")} value={formatCount(f.eligible)} hint={t("insights.fg.eligibleHint")} />
          <Tile label={t("insights.fg.exempt")} value={formatCount(f.exempt)} hint={t("insights.fg.exemptHint")} />
        </div>
        <div>
          <p className="mb-2 text-sm font-medium">{t("insights.fg.histogram")}</p>
          <BarList items={f.retention_histogram} empty={t("insights.fg.histogramEmpty")} />
        </div>
        {f.shadow ? <ShadowSection s={f.shadow} /> : null}
      </div>
    </Section>
  );
}

// Old versus new retention model, side by side (D39 shadow mode): how many
// eligible memories each would forget by each horizon if nothing reads them.
function ShadowSection({ s }: { s: RetentionShadow }) {
  const { t } = useT();
  const horizons = s.horizons_days;
  const rows = s.by_kind.filter((k) => k.eligible > 0);
  const cell = (legacy: number[], perKind: number[], i: number) => (
    <TableCell key={i} className="text-right tabular-nums">
      {formatCount(legacy[i] ?? 0)} <span className="text-muted-foreground">{t("insights.sh.to")}</span> {formatCount(perKind[i] ?? 0)}
      <span className="block text-xs text-muted-foreground">{shadowDelta(legacy[i] ?? 0, perKind[i] ?? 0, t)}</span>
    </TableCell>
  );
  return (
    <div>
      <p className="mb-1 text-sm font-medium">{t("insights.sh.title")}</p>
      <p className="mb-2 text-xs text-muted-foreground">
        {t("insights.sh.intro")} <strong>{s.active}</strong> {t("insights.sh.outro")}
        {s.protected > 0 ? ` ${t("insights.sh.protected", { count: formatCount(s.protected) })}` : ""}
        {s.expired_cap_enabled
          ? ` ${t("insights.sh.capOn", { days: s.expired_cap_days })}`
          : ` ${t("insights.sh.capOff")}`}
      </p>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t("insights.sh.colKind")}</TableHead>
            <TableHead className="text-right">{t("insights.sh.colHalfLife")}</TableHead>
            <TableHead className="text-right">{t("insights.sh.colEligible")}</TableHead>
            {horizons.map((h) => (
              <TableHead key={h} className="text-right">
                {horizonLabel(h, t)}
              </TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((k) => (
            <TableRow key={k.kind}>
              <TableCell>{k.kind}</TableCell>
              <TableCell className="text-right tabular-nums">{s.half_life_days[k.kind] ?? "-"} {t("insights.unit.days")}</TableCell>
              <TableCell className="text-right tabular-nums">{formatCount(k.eligible)}</TableCell>
              {horizons.map((_, i) => cell(k.legacy, k.per_kind, i))}
            </TableRow>
          ))}
          <TableRow className="font-medium">
            <TableCell>{t("insights.sh.all")}</TableCell>
            <TableCell />
            <TableCell className="text-right tabular-nums">{formatCount(s.eligible)}</TableCell>
            {horizons.map((_, i) => cell(s.legacy, s.per_kind, i))}
          </TableRow>
        </TableBody>
      </Table>
    </div>
  );
}

function FilterSection({ f }: { f: FilterOverview }) {
  const { t } = useT();
  return (
    <Section
      title={t("insights.fl.title")}
      description={t("insights.fl.desc", { mode: f.mode, version: f.rules_version })}
    >
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t("insights.fl.colRule")}</TableHead>
            <TableHead>{t("insights.fl.colScope")}</TableHead>
            <TableHead>{t("insights.fl.colAction")}</TableHead>
            <TableHead className="text-right">{t("insights.fl.colSeen")}</TableHead>
            <TableHead className="text-right">{t("insights.fl.colDropped")}</TableHead>
            <TableHead className="text-right">{t("insights.fl.colTagged")}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {f.rules.map((r) => (
            <TableRow key={r.id}>
              <TableCell className="font-mono text-xs" title={r.description}>
                {r.id}
              </TableCell>
              <TableCell>{r.scope}</TableCell>
              <TableCell>{r.action}</TableCell>
              <TableCell className="text-right tabular-nums">{formatCount(r.seen)}</TableCell>
              <TableCell className="text-right tabular-nums">{formatCount(r.dropped)}</TableCell>
              <TableCell className="text-right tabular-nums">{formatCount(r.tagged)}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </Section>
  );
}

// ── Hygiene analysis ──

function ExampleList({ examples }: { examples: ExampleEntry[] }) {
  const { t } = useT();
  if (examples.length === 0) return <p className="text-sm text-muted-foreground">{t("insights.hy.noExample")}</p>;
  return (
    <ul className="flex flex-col gap-2">
      {examples.map((e) => (
        <li key={e.id} className="rounded border p-2 text-sm">
          <p>{e.text}</p>
          <p className="mt-1 font-mono text-xs text-muted-foreground">
            {e.id} · {e.domain} · {e.source}
          </p>
        </li>
      ))}
    </ul>
  );
}

function Expandable({ summary, children }: { summary: string; children: React.ReactNode }) {
  return (
    <details className="group rounded border px-3 py-2">
      <summary className="cursor-pointer select-none text-sm text-muted-foreground group-open:text-foreground">
        {summary}
      </summary>
      <div className="mt-2">{children}</div>
    </details>
  );
}

function RunControls({ status, report }: { status: HygieneStatus; report: HygieneReport | null }) {
  const { t, tag } = useT();
  const start = useStartHygiene();
  const cancel = useCancelHygiene();
  const pct = progressPercent(status.done, status.total);

  function run() {
    start.mutate(undefined, { onError: (e) => toast.error(e instanceof Error ? e.message : String(e)) });
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-3">
        {status.running ? (
          <Button variant="destructive" size="sm" disabled={cancel.isPending} onClick={() => cancel.mutate()}>
            <Square data-icon="inline-start" />
            {t("insights.hy.cancel")}
          </Button>
        ) : (
          <Button size="sm" disabled={start.isPending} onClick={run}>
            <Play data-icon="inline-start" />
            {t("insights.hy.run")}
          </Button>
        )}
        {report ? (
          <span className="text-sm text-muted-foreground">
            {t("insights.hy.lastRun", {
              date: formatDateTime(report.computed_at, tag),
              duration: formatDuration(report.duration_ms),
              count: formatCount(report.scanned),
            })}
          </span>
        ) : (
          <span className="text-sm text-muted-foreground">{t("insights.hy.neverRun")}</span>
        )}
      </div>

      {status.running ? (
        <div className="flex flex-col gap-1.5" data-testid="hygiene-progress">
          <div
            role="progressbar"
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={pct}
            className="h-2 w-full overflow-hidden rounded-full bg-muted"
          >
            <div className="h-full bg-primary transition-all" style={{ width: `${pct}%` }} />
          </div>
          <p className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="size-3.5 animate-spin" />
            {status.phase || t("insights.hy.starting")}
            {status.total > 0 ? ` · ${pct}%` : ""}
          </p>
        </div>
      ) : null}

      {!status.running && status.last_error ? (
        <Alert variant="destructive">
          <AlertTitle>{t("insights.hy.lastFailed")}</AlertTitle>
          <AlertDescription>{status.last_error}</AlertDescription>
        </Alert>
      ) : null}

      {report?.partial ? (
        <Alert>
          <AlertTriangle />
          <AlertTitle>{t("insights.hy.partial")}</AlertTitle>
          <AlertDescription>{report.partial_reason}</AlertDescription>
        </Alert>
      ) : null}
    </div>
  );
}

function ExactSection({ report }: { report: HygieneReport }) {
  const { t } = useT();
  const x = report.exact_duplicates;
  return (
    <Section
      title={t("insights.ex.title")}
      description={t("insights.ex.desc")}
    >
      <div className="flex flex-col gap-4">
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <Tile label={t("insights.ex.groups")} value={formatCount(x.groups)} hint={t("insights.ex.groupsHint", { count: formatCount(x.entries) })} />
          <Tile label={t("insights.ex.redundant")} value={formatCount(x.redundant)} />
          <Tile
            label={t("insights.ex.sameCross")}
            value={`${formatCount(x.same_domain_groups)} / ${formatCount(x.cross_domain_groups)}`}
            hint={t("insights.ex.groupsUnit")}
          />
          <Tile label={t("insights.acrossTools")} value={formatCount(x.cross_source_groups)} hint={t("insights.ex.mixedHint")} />
        </div>
        {x.examples.length > 0 ? (
          <Expandable summary={t("insights.ex.largest", { count: x.examples.length })}>
            <ul className="flex flex-col gap-2">
              {x.examples.map((g) => (
                <li key={g.members.map((m) => m.id).join()} className="rounded border p-2 text-sm">
                  <p>{g.text}</p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    {t("insights.ex.copies", { count: g.size })} · {t("insights.domainsCount", { count: g.domains })} ·{" "}
                    {g.members.map((m) => domainInstance(m.domain)).join(", ")}
                  </p>
                </li>
              ))}
            </ul>
          </Expandable>
        ) : null}
      </div>
    </Section>
  );
}

function NearSection({ report }: { report: HygieneReport }) {
  const { t, tag } = useT();
  const n = report.near_duplicates;
  if (!n.available) {
    return (
      <Section title={t("insights.nr.title")} description={t("insights.nr.descUnavailable")}>
        <p className="text-sm text-muted-foreground">
          {t("insights.nr.unavailable", { reason: n.reason ?? t("insights.nr.noVectors") })}
        </p>
      </Section>
    );
  }
  return (
    <Section
      title={t("insights.nr.title")}
      description={t("insights.nr.desc", {
        count: formatCount(n.indexed ?? 0),
        method: n.method === "lsh" ? t("insights.nr.methodLsh") : t("insights.nr.methodAll"),
      })}
    >
      <div className="flex flex-col gap-4">
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("insights.nr.colCosine")}</TableHead>
                <TableHead className="text-right">{t("insights.nr.colPairs")}</TableHead>
                <TableHead className="text-right">{t("insights.nr.colClusters")}</TableHead>
                <TableHead className="text-right">{t("insights.nr.colRedundant")}</TableHead>
                <TableHead className="text-right">{t("insights.nr.colSameDomain")}</TableHead>
                <TableHead className="text-right">{t("insights.nr.colAcrossDomains")}</TableHead>
                <TableHead className="text-right">{t("insights.acrossTools")}</TableHead>
                <TableHead className="text-right">{t("insights.nr.colSameCapture")}</TableHead>
                <TableHead className="text-right">{t("insights.nr.colNumbersDiffer")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {n.thresholds.map((th) => (
                <TableRow key={th.threshold}>
                  <TableCell>≥ {formatDecimal(th.threshold, tag, 2)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatCount(th.pairs)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatCount(th.clusters)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatCount(th.redundant)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatCount(th.same_domain)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatCount(th.cross_domain)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatCount(th.cross_source)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatCount(th.same_capture)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatCount(th.numbers_differ)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
        <p className="text-xs text-muted-foreground">
          {t("insights.nr.legend")}
          {n.cross_source.length > 0
            ? ` ${t("insights.nr.acrossToolsList", { list: n.cross_source.map((c) => `${c.key} (${c.count})`).join(", ") })}`
            : ""}
          {n.pairs_truncated ? ` ${t("insights.nr.truncated")}` : ""}
        </p>
        {n.bands.map((b) => (
          <Expandable key={b.label} summary={t("insights.nr.bandSummary", { label: b.label, count: b.examples.length })}>
            {b.examples.length === 0 ? (
              <p className="text-sm text-muted-foreground">{t("insights.nr.noPair")}</p>
            ) : (
              <ul className="flex flex-col gap-2">
                {b.examples.map((p) => (
                  <li key={p.a.id + p.b.id} className="rounded border p-2 text-sm">
                    <Badge variant="outline">{t("insights.nr.cosine", { value: formatDecimal(p.similarity, tag, 3) })}</Badge>
                    <p className="mt-1">{p.a.text}</p>
                    <p className="text-xs text-muted-foreground">
                      {p.a.id} · {p.a.domain} · {p.a.source}
                    </p>
                    <p className="mt-1">{p.b.text}</p>
                    <p className="text-xs text-muted-foreground">
                      {p.b.id} · {p.b.domain} · {p.b.source}
                    </p>
                  </li>
                ))}
              </ul>
            )}
          </Expandable>
        ))}
      </div>
    </Section>
  );
}

function CriterionRow({ c, scanned }: { c: NoiseCriterion; scanned: number }) {
  const { t } = useT();
  return (
    <div className="flex flex-col gap-2 rounded-lg border p-3">
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant="outline">{c.id}</Badge>
        <span className="font-medium">{c.name}</span>
        <Badge variant={c.tier === "conservative" ? "secondary" : "outline"}>{tierLabel(c.tier, t)}</Badge>
        <span className="ml-auto text-sm tabular-nums">
          {formatCount(c.matches)} <span className="text-muted-foreground">({formatPercent(c.matches, scanned)})</span>
        </span>
      </div>
      <p className="text-sm text-muted-foreground">{c.description}</p>
      <p className="text-xs text-muted-foreground">{t("insights.nz.neverAccessed", { count: formatCount(c.never_accessed) })}</p>
      {c.examples.length > 0 ? (
        <Expandable summary={t("insights.nz.examples", { count: c.examples.length })}>
          <ExampleList examples={c.examples} />
        </Expandable>
      ) : null}
    </div>
  );
}

function NoiseSection({ report }: { report: HygieneReport }) {
  const { t } = useT();
  const n = report.noise;
  return (
    <Section
      title={t("insights.nz.title", { category: n.category })}
      description={t("insights.nz.desc")}
    >
      <div className="flex flex-col gap-3">
        <div className="grid gap-3 sm:grid-cols-3">
          <Tile
            label={t("insights.nz.conservative")}
            value={formatCount(n.conservative_union)}
            hint={t("insights.nz.conservativeHint", { count: formatCount(n.conservative_eligible) })}
          />
          <Tile label={t("insights.tier.review")} value={formatCount(n.review_union)} hint={t("insights.nz.reviewHint")} />
          <Tile label={t("insights.nz.scanned")} value={formatCount(n.scanned)} hint={t("insights.nz.scannedHint", { category: n.category })} />
        </div>
        {n.criteria.map((c) => (
          <CriterionRow key={c.id} c={c} scanned={n.scanned} />
        ))}
      </div>
    </Section>
  );
}

function HygieneSection() {
  const { t } = useT();
  const { data, isLoading, error } = useHygiene();
  return (
    <Section
      title={t("insights.hy.title")}
      description={t("insights.hy.desc")}
    >
      {isLoading ? (
        <Skeleton className="h-24 w-full" />
      ) : error || !data ? (
        <Alert variant="destructive">
          <AlertTitle>{t("insights.hy.loadError")}</AlertTitle>
          <AlertDescription>{error instanceof Error ? error.message : t("insights.unknownError")}</AlertDescription>
        </Alert>
      ) : (
        <div className="flex flex-col gap-4">
          <RunControls status={data.status} report={data.report} />
          {data.report === null ? (
            <Empty className="border">
              <EmptyHeader>
                <EmptyTitle>{t("insights.hy.emptyTitle")}</EmptyTitle>
                <EmptyDescription>
                  {t("insights.hy.emptyDesc")}
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <div className="flex flex-col gap-4">
              <ExactSection report={data.report} />
              <NearSection report={data.report} />
              <NoiseSection report={data.report} />
              <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <CheckCircle2 className="size-3.5" />
                {t("insights.hy.footnote")}
              </p>
            </div>
          )}
        </div>
      )}
    </Section>
  );
}

export default function InsightsPage() {
  const { t, tag } = useT();
  const { data, isLoading, error } = useInsightsOverview();
  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title={t("insights.page.title")}
        subtitle={`${t("insights.page.subtitle")}${data ? ` ${t("insights.page.updated", { date: formatDateTime(data.generated_at, tag) })}` : ""}`}
        help="insights"
        term="insights"
      />
      <nav aria-label={t("insights.jump.label")} className="flex flex-wrap gap-x-4 gap-y-1 text-sm">
        {(["overview", "forgetting", "filter", "hygiene"] as const).map((id) => (
          <a key={id} href={`#${id}`} className="text-primary underline-offset-4 hover:underline">
            {t(`insights.jump.${id}`)}
          </a>
        ))}
      </nav>
      {isLoading ? (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          {Array.from({ length: 8 }, (_, i) => (
            <Skeleton key={i} className="h-20 w-full" />
          ))}
        </div>
      ) : error || !data ? (
        <Alert variant="destructive">
          <AlertTitle>{t("insights.page.loadError")}</AlertTitle>
          <AlertDescription>{error instanceof Error ? error.message : t("insights.unknownError")}</AlertDescription>
        </Alert>
      ) : (
        <>
          <HealthSummary ov={data} />
          <div id="overview" className="flex scroll-mt-4 flex-col gap-4">
            <OverviewSection ov={data} />
          </div>
          <div id="forgetting" className="scroll-mt-4">
            <ForgettingSection f={data.forgetting} />
          </div>
          {data.filter ? (
            <div id="filter" className="scroll-mt-4">
              <FilterSection f={data.filter} />
            </div>
          ) : null}
        </>
      )}
      <div id="hygiene" className="scroll-mt-4">
        <HygieneSection />
      </div>
    </div>
  );
}
