// Memory health dashboard (D37). The top half is live and cheap (GET
// /insights/overview: totals, distributions, forgetting outlook). The bottom
// half is the hygiene analysis (GET /insights/hygiene): a read-only job that
// looks for duplicates and low-value memories and stores its last result, so
// the page shows when it was computed and never recomputes on its own.
// Charts are plain CSS bars: no chart library is needed for this.
import { AlertTriangle, CheckCircle2, Loader2, Play, Square } from "lucide-react";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useCancelHygiene, useHygiene, useInsightsOverview, useStartHygiene } from "@/hooks/useInsightsApi";
import {
  barWidths,
  domainInstance,
  formatCount,
  formatDuration,
  formatPercent,
  progressPercent,
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
} from "@/lib/schemas";

const dateTimeFormatter = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });
const formatDateTime = (iso: string) => dateTimeFormatter.format(new Date(iso));

function Tile({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="flex flex-col rounded-lg border p-3">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span className="text-2xl font-semibold leading-tight">{value}</span>
      {hint ? <span className="text-xs text-muted-foreground">{hint}</span> : null}
    </div>
  );
}

function BarList({ items, empty = "No data." }: { items: CountItem[]; empty?: string }) {
  if (items.length === 0) return <p className="text-sm text-muted-foreground">{empty}</p>;
  const widths = barWidths(items);
  return (
    <ul className="flex flex-col gap-1.5">
      {items.map((item, i) => (
        <li key={item.key} className="grid grid-cols-[minmax(0,10rem)_1fr_auto] items-center gap-2 text-sm">
          <span className="truncate" title={item.key}>
            {item.key}
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
  if (items.length === 0) return <p className="text-sm text-muted-foreground">Nothing was soft-deleted yet.</p>;
  const widths = barWidths(items);
  return (
    <div>
      <div className="flex h-24 items-end gap-0.5" role="img" aria-label="Soft-deleted memories per day">
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

// ── Overview ──

function OverviewSection({ ov }: { ov: InsightsOverview }) {
  const t = ov.totals;
  return (
    <>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <Tile label="Active memories" value={formatCount(t.active)} hint={`${formatCount(t.total)} stored in total`} />
        <Tile
          label="Soft-deleted"
          value={formatCount(t.soft_deleted)}
          hint="hidden, restorable until hard-deleted"
        />
        <Tile
          label="Never accessed"
          value={`${t.never_accessed_pct.toFixed(1)}%`}
          hint={`${formatCount(t.never_accessed)} of ${formatCount(t.active)} active`}
        />
        <Tile
          label="Past valid_until"
          value={formatCount(t.expired)}
          hint={`${formatCount(t.with_valid_until)} have a valid_until · ${formatPercent(t.expired, t.active)} of active`}
        />
        <Tile label="Pinned" value={formatCount(t.pinned)} hint="exempt from forgetting" />
        <Tile
          label="Extracted facts"
          value={formatCount(t.extracted)}
          hint={`${formatCount(t.raw)} raw · ${formatPercent(t.extracted, t.active)} extracted`}
        />
        <Tile
          label="Average text length"
          value={`${Math.round(ov.text.avg_chars)} chars`}
          hint={`median ${ov.text.median_chars} · p90 ${ov.text.p90_chars} · max ${formatCount(ov.text.max_chars)}`}
        />
        <Tile label="Vectors indexed" value={formatCount(ov.vectors_indexed)} hint={`${formatCount(ov.domain_count)} domains`} />
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <Section title="By age" description="Active memories by time since creation.">
          <BarList items={ov.by_age} />
        </Section>
        <Section title="By source (tool)" description="Which tool wrote the active memories.">
          <BarList items={ov.by_source} />
        </Section>
        <Section title="By domain category">
          <BarList items={ov.by_category} />
        </Section>
        <Section title="Soft-deleted per day" description="Memories hidden by forgetting, by deletion day.">
          <DayBars items={ov.soft_deleted_by_day} />
        </Section>
      </div>

      <Section title="Top domains" description={`${formatCount(ov.domain_count)} domains in total.`}>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Domain</TableHead>
              <TableHead className="text-right">Memories</TableHead>
              <TableHead className="text-right">Never accessed</TableHead>
              <TableHead className="text-right">Past valid_until</TableHead>
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
  return (
    <Section
      title="Forgetting"
      description={`What the next forget-sweep would do, with the same retention score (age half-life ${f.age_half_life_days} days, cold below ${f.threshold}). Nothing is changed here.`}
    >
      <div className="flex flex-col gap-4">
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <Tile label="Cold right now" value={formatCount(f.cold_now)} hint="would be soft-deleted by a sweep" />
          <Tile
            label="Waiting for hard-delete"
            value={formatCount(f.soft_deleted)}
            hint={`${formatCount(f.hard_delete_due)} already past the ${f.grace_days}-day grace window`}
          />
          <Tile label="Eligible to decay" value={formatCount(f.eligible)} hint="extracted and not pinned" />
          <Tile label="Exempt" value={formatCount(f.exempt)} hint="raw content or pinned" />
        </div>
        <div>
          <p className="mb-2 text-sm font-medium">Retention score of eligible memories</p>
          <BarList items={f.retention_histogram} empty="No memory is eligible to decay." />
        </div>
      </div>
    </Section>
  );
}

function FilterSection({ f }: { f: FilterOverview }) {
  return (
    <Section
      title="Ingestion filter"
      description={`Tooling boilerplate stopped at automatic capture (mode ${f.mode}, rules v${f.rules_version}). Counters reset when the server restarts. Explicit writes and pinned memories are never filtered.`}
    >
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Rule</TableHead>
            <TableHead>Scope</TableHead>
            <TableHead>Action</TableHead>
            <TableHead className="text-right">Seen</TableHead>
            <TableHead className="text-right">Dropped</TableHead>
            <TableHead className="text-right">Tagged</TableHead>
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
  if (examples.length === 0) return <p className="text-sm text-muted-foreground">No example to show.</p>;
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
            Cancel
          </Button>
        ) : (
          <Button size="sm" disabled={start.isPending} onClick={run}>
            <Play data-icon="inline-start" />
            Run analysis
          </Button>
        )}
        {report ? (
          <span className="text-sm text-muted-foreground">
            Last run {formatDateTime(report.computed_at)} · took {formatDuration(report.duration_ms)} ·{" "}
            {formatCount(report.scanned)} memories scanned
          </span>
        ) : (
          <span className="text-sm text-muted-foreground">Never run.</span>
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
            {status.phase || "Starting"}
            {status.total > 0 ? ` · ${pct}%` : ""}
          </p>
        </div>
      ) : null}

      {!status.running && status.last_error ? (
        <Alert variant="destructive">
          <AlertTitle>The last run did not finish</AlertTitle>
          <AlertDescription>{status.last_error}</AlertDescription>
        </Alert>
      ) : null}

      {report?.partial ? (
        <Alert>
          <AlertTriangle />
          <AlertTitle>Partial result</AlertTitle>
          <AlertDescription>{report.partial_reason}</AlertDescription>
        </Alert>
      ) : null}
    </div>
  );
}

function ExactSection({ report }: { report: HygieneReport }) {
  const x = report.exact_duplicates;
  return (
    <Section
      title="Identical text"
      description="Memories whose text is the same after ignoring case, accents and punctuation."
    >
      <div className="flex flex-col gap-4">
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <Tile label="Groups" value={formatCount(x.groups)} hint={`${formatCount(x.entries)} memories involved`} />
          <Tile label="Redundant copies" value={formatCount(x.redundant)} />
          <Tile
            label="Same domain / across domains"
            value={`${formatCount(x.same_domain_groups)} / ${formatCount(x.cross_domain_groups)}`}
            hint="groups"
          />
          <Tile label="Across tools" value={formatCount(x.cross_source_groups)} hint="groups with mixed sources" />
        </div>
        {x.examples.length > 0 ? (
          <Expandable summary={`Largest groups (${x.examples.length})`}>
            <ul className="flex flex-col gap-2">
              {x.examples.map((g) => (
                <li key={g.members.map((m) => m.id).join()} className="rounded border p-2 text-sm">
                  <p>{g.text}</p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    {g.size} copies · {g.domains} domain{g.domains === 1 ? "" : "s"} ·{" "}
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
  const n = report.near_duplicates;
  if (!n.available) {
    return (
      <Section title="Near-duplicates" description="Memories that mean the same thing in different words.">
        <p className="text-sm text-muted-foreground">Not available: {n.reason ?? "no vectors."}</p>
      </Section>
    );
  }
  return (
    <Section
      title="Near-duplicates"
      description={`Cosine similarity between memory vectors (${formatCount(n.indexed ?? 0)} compared, ${
        n.method === "lsh" ? "approximate hashing" : "all pairs"
      }). Pairs are counted at or above each threshold.`}
    >
      <div className="flex flex-col gap-4">
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Cosine</TableHead>
                <TableHead className="text-right">Pairs</TableHead>
                <TableHead className="text-right">Clusters</TableHead>
                <TableHead className="text-right">Redundant</TableHead>
                <TableHead className="text-right">Same domain</TableHead>
                <TableHead className="text-right">Across domains</TableHead>
                <TableHead className="text-right">Across tools</TableHead>
                <TableHead className="text-right">Same capture</TableHead>
                <TableHead className="text-right">Numbers differ</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {n.thresholds.map((th) => (
                <TableRow key={th.threshold}>
                  <TableCell>≥ {th.threshold.toFixed(2)}</TableCell>
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
          Redundant = copies beyond one per cluster. Same capture = both facts came from the same source text. Numbers
          differ = the texts hold different numbers, so they are probably distinct facts of the same shape rather than
          duplicates.
          {n.cross_source.length > 0
            ? ` Across tools: ${n.cross_source.map((c) => `${c.key} (${c.count})`).join(", ")}.`
            : ""}
          {n.pairs_truncated ? " Pair collection hit its safety cap, so counts are a lower bound." : ""}
        </p>
        {n.bands.map((b) => (
          <Expandable key={b.label} summary={`${b.label} (${b.examples.length} example${b.examples.length === 1 ? "" : "s"})`}>
            {b.examples.length === 0 ? (
              <p className="text-sm text-muted-foreground">No pair in this band.</p>
            ) : (
              <ul className="flex flex-col gap-2">
                {b.examples.map((p) => (
                  <li key={p.a.id + p.b.id} className="rounded border p-2 text-sm">
                    <Badge variant="outline">cosine {p.similarity.toFixed(3)}</Badge>
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
  return (
    <div className="flex flex-col gap-2 rounded-lg border p-3">
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant="outline">{c.id}</Badge>
        <span className="font-medium">{c.name}</span>
        <Badge variant={c.tier === "conservative" ? "secondary" : "outline"}>{tierLabel(c.tier)}</Badge>
        <span className="ml-auto text-sm tabular-nums">
          {formatCount(c.matches)} <span className="text-muted-foreground">({formatPercent(c.matches, scanned)})</span>
        </span>
      </div>
      <p className="text-sm text-muted-foreground">{c.description}</p>
      <p className="text-xs text-muted-foreground">{formatCount(c.never_accessed)} never accessed</p>
      {c.examples.length > 0 ? (
        <Expandable summary={`Examples (${c.examples.length})`}>
          <ExampleList examples={c.examples} />
        </Expandable>
      ) : null}
    </div>
  );
}

function NoiseSection({ report }: { report: HygieneReport }) {
  const n = report.noise;
  return (
    <Section
      title={`Low-value candidates in “${n.category}”`}
      description="Patterns that usually mean noise. This only reports: nothing is flagged or deleted. Conservative patterns are narrow; the others need a human look."
    >
      <div className="flex flex-col gap-3">
        <div className="grid gap-3 sm:grid-cols-3">
          <Tile
            label="Conservative candidates"
            value={formatCount(n.conservative_union)}
            hint={`${formatCount(n.conservative_eligible)} also never accessed, not pinned, extracted`}
          />
          <Tile label="For review" value={formatCount(n.review_union)} hint="matches that need a human decision" />
          <Tile label="Scanned" value={formatCount(n.scanned)} hint={`“${n.category}” memories`} />
        </div>
        {n.criteria.map((c) => (
          <CriterionRow key={c.id} c={c} scanned={n.scanned} />
        ))}
      </div>
    </Section>
  );
}

function HygieneSection() {
  const { data, isLoading, error } = useHygiene();
  return (
    <Section
      title="Hygiene analysis"
      description="Looks for duplicates and low-value memories. It only reads: it never changes or deletes anything. It runs on demand and keeps its last result, so this page is cheap to open."
    >
      {isLoading ? (
        <Skeleton className="h-24 w-full" />
      ) : error || !data ? (
        <Alert variant="destructive">
          <AlertTitle>Could not load the analysis</AlertTitle>
          <AlertDescription>{error instanceof Error ? error.message : "Unknown error"}</AlertDescription>
        </Alert>
      ) : (
        <div className="flex flex-col gap-4">
          <RunControls status={data.status} report={data.report} />
          {data.report === null ? (
            <Empty className="border">
              <EmptyHeader>
                <EmptyTitle>No analysis yet</EmptyTitle>
                <EmptyDescription>
                  Run it once to see identical and near-duplicate memories and low-value candidates. It needs no
                  network and uses little memory; large collections take a few minutes.
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
                Examples are truncated and exclude anything that looks like a secret.
              </p>
            </div>
          )}
        </div>
      )}
    </Section>
  );
}

export default function InsightsPage() {
  const { data, isLoading, error } = useInsightsOverview();
  return (
    <div className="flex flex-col gap-4">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Memory health</h1>
        <p className="text-sm text-muted-foreground">
          How the collection looks, what would be forgotten next, and where duplicates and noise hide.
          {data ? ` Updated ${formatDateTime(data.generated_at)}.` : ""}
        </p>
      </div>
      {isLoading ? (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          {Array.from({ length: 8 }, (_, i) => (
            <Skeleton key={i} className="h-20 w-full" />
          ))}
        </div>
      ) : error || !data ? (
        <Alert variant="destructive">
          <AlertTitle>Could not load the overview</AlertTitle>
          <AlertDescription>{error instanceof Error ? error.message : "Unknown error"}</AlertDescription>
        </Alert>
      ) : (
        <>
          <OverviewSection ov={data} />
          <ForgettingSection f={data.forgetting} />
          {data.filter ? <FilterSection f={data.filter} /> : null}
        </>
      )}
      <HygieneSection />
    </div>
  );
}
