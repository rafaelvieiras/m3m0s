// Embedding settings (D36, issue #39 phase 3): provider/model/dimensions/key
// form, the re-embedding job controls, and the confirmation flow when a change
// leaves stored vectors behind (409 → estimate → explicit confirmation).
// The API key is write-only: the server never returns it, so the field starts
// empty and an empty field means "keep the saved key".
import { useState } from "react";
import { CheckCircle2, Loader2, Play, Save, Square } from "lucide-react";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
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
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import {
  useCancelReembed,
  useEmbeddingConfig,
  useEstimateReembed,
  useSaveEmbeddingConfig,
  useStartReembed,
} from "@/hooks/useEmbeddingApi";
import { useT } from "@/i18n";
import { ApiError } from "@/lib/api";
import { buildPayload, formatCost, formatEta, progressPercent, type EmbeddingFormState } from "@/lib/embedding";
import type { EmbeddingConfig, EmbeddingConfigPayload, ReembedEstimate } from "@/lib/schemas";
import { getEmbeddingConfig } from "@/lib/api";

interface PendingConfirmation {
  payload: EmbeddingConfigPayload;
  estimate: ReembedEstimate;
  stale: number;
  total: number;
}

function EstimateSummary({ est }: { est: ReembedEstimate }) {
  const { t, tag } = useT();
  const n = (v: number) => v.toLocaleString(tag);
  return (
    <ul className="list-disc pl-5 text-sm">
      <li>
        {t("embedding.estimate.pending", { pending: n(est.pending), model: "\u0000" })
          .split("\u0000")
          .map((part, i, arr) => (
            <span key={i}>
              {part}
              {i < arr.length - 1 ? <code>{est.model}</code> : null}
            </span>
          ))}
      </li>
      <li>
        {t("embedding.estimate.tokens", { tokens: n(est.estimated_tokens), characters: n(est.characters) })}
      </li>
      <li>{t("embedding.estimate.cost", { cost: formatCost(est) })}</li>
    </ul>
  );
}

function ConfigForm({ config }: { config: EmbeddingConfig }) {
  const { t, tag } = useT();
  const save = useSaveEmbeddingConfig();
  const estimate = useEstimateReembed();
  const [form, setForm] = useState<EmbeddingFormState>(() => ({
    enabled: config.enabled,
    baseUrl: config.base_url,
    model: config.model,
    dimensions: config.dimensions > 0 ? String(config.dimensions) : "",
    apiKey: "",
    clearKey: false,
  }));
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState<PendingConfirmation | null>(null);

  function set<K extends keyof EmbeddingFormState>(key: K, value: EmbeddingFormState[K]) {
    setForm((f) => ({ ...f, [key]: value }));
  }

  async function submit(payload: EmbeddingConfigPayload) {
    setError(null);
    try {
      const res = await save.mutateAsync(payload);
      setPending(null);
      setForm((f) => ({ ...f, apiKey: "", clearKey: false }));
      toast.success(res.reembed_started ? t("embedding.toast.savedReembed") : t("embedding.toast.saved"));
    } catch (e) {
      if (e instanceof ApiError && e.status === 409 && e.code === "reembed_required") {
        // Stored vectors would become stale: show how many and what it costs,
        // and only proceed on an explicit confirmation.
        try {
          const [est, cfg] = await Promise.all([
            estimate.mutateAsync({ model: payload.model, base_url: payload.base_url, dimensions: payload.dimensions }),
            getEmbeddingConfig(),
          ]);
          setPending({ payload, estimate: est, stale: est.pending, total: cfg.vectors.total });
        } catch (e2) {
          setError(e2 instanceof Error ? e2.message : String(e2));
        }
        return;
      }
      setPending(null);
      // 422: the provider rejected the settings or returned a different size;
      // the active configuration is unchanged.
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  function onSubmit(ev: React.FormEvent) {
    ev.preventDefault();
    const p = buildPayload(form);
    if ("error" in p) {
      setError(p.error);
      return;
    }
    void submit(p);
  }

  const keyLabel = config.has_api_key ? t("embedding.keyLabel.set", { source: config.key_source }) : t("embedding.keyLabel.notSet");

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("embedding.provider.title")}</CardTitle>
        <CardDescription>
          {t("embedding.provider.description")}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={onSubmit} className="flex flex-col gap-4">
          <FieldGroup>
            <Field orientation="horizontal">
              <input
                id="emb-enabled"
                type="checkbox"
                className="size-4"
                checked={form.enabled}
                onChange={(e) => set("enabled", e.target.checked)}
              />
              <FieldLabel htmlFor="emb-enabled">{t("embedding.form.enabled")}</FieldLabel>
            </Field>
            <Field>
              <FieldLabel htmlFor="emb-base">{t("embedding.form.baseUrl")}</FieldLabel>
              <Input
                id="emb-base"
                value={form.baseUrl}
                placeholder={t("embedding.form.baseUrlPlaceholder")}
                onChange={(e) => set("baseUrl", e.target.value)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="emb-model">{t("embedding.form.model")}</FieldLabel>
              <Input id="emb-model" value={form.model} onChange={(e) => set("model", e.target.value)} />
            </Field>
            <Field>
              <FieldLabel htmlFor="emb-dims">{t("embedding.form.dimensions")}</FieldLabel>
              <Input
                id="emb-dims"
                inputMode="numeric"
                value={form.dimensions}
                placeholder={t("embedding.form.dimensionsPlaceholder")}
                onChange={(e) => set("dimensions", e.target.value)}
              />
              <FieldDescription>
                {config.vector_dims > 0
                  ? t("embedding.form.dimensionsHelpCurrent", { dims: config.vector_dims })
                  : t("embedding.form.dimensionsHelp")}
              </FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="emb-key">{t("embedding.form.apiKey")}</FieldLabel>
              <Input
                id="emb-key"
                type="password"
                autoComplete="new-password"
                value={form.apiKey}
                disabled={form.clearKey}
                placeholder={config.has_api_key ? t("embedding.form.apiKeyKeep") : t("embedding.form.apiKeyPaste")}
                onChange={(e) => set("apiKey", e.target.value)}
              />
              <FieldDescription>
                {t("embedding.form.apiKeyHelp", { key: keyLabel })}
              </FieldDescription>
              {config.key_source === "database" ? (
                <label className="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    className="size-4"
                    checked={form.clearKey}
                    onChange={(e) => set("clearKey", e.target.checked)}
                  />
                  {t("embedding.form.clearKey")}
                </label>
              ) : null}
            </Field>
          </FieldGroup>

          {error ? (
            <Alert variant="destructive">
              <AlertTitle>{t("embedding.form.notSaved")}</AlertTitle>
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}

          <div>
            <Button type="submit" disabled={save.isPending || estimate.isPending}>
              {save.isPending ? <Loader2 data-icon="inline-start" className="animate-spin" /> : <Save data-icon="inline-start" />}
              {t("embedding.form.save")}
            </Button>
          </div>
        </form>
      </CardContent>

      <AlertDialog open={pending !== null} onOpenChange={(o) => (!o ? setPending(null) : undefined)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("embedding.confirm.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {pending
                ? t("embedding.confirm.description", {
                    stale: pending.stale.toLocaleString(tag),
                    total: pending.total.toLocaleString(tag),
                  })
                : null}
            </AlertDialogDescription>
          </AlertDialogHeader>
          {pending ? <EstimateSummary est={pending.estimate} /> : null}
          <AlertDialogFooter>
            <AlertDialogCancel>{t("embedding.confirm.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              disabled={save.isPending}
              onClick={(e) => {
                e.preventDefault(); // keep the dialog open until the save settles
                if (pending) void submit({ ...pending.payload, confirm_reembed: true });
              }}
            >
              {save.isPending ? t("embedding.confirm.saving") : t("embedding.confirm.saveAndReembed")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  );
}

function ReembedCard({ config }: { config: EmbeddingConfig }) {
  const { t, tag } = useT();
  const start = useStartReembed();
  const cancel = useCancelReembed();
  const [confirmStart, setConfirmStart] = useState<ReembedEstimate | null>(null);
  const estimate = useEstimateReembed();
  const p = config.reembed;
  const stale = config.vectors.stale;
  const pct = progressPercent(p.done + p.skipped, p.total);
  const upToDate = stale === 0 && !p.running;

  async function askStart() {
    try {
      setConfirmStart(await estimate.mutateAsync({}));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e));
    }
  }

  return (
    <Card>
      <CardHeader className="flex-row items-start justify-between gap-4 space-y-0">
        <div>
          <CardTitle>{t("embedding.reembed.title")}</CardTitle>
          <CardDescription>
            {t("embedding.reembed.summary", {
              current: config.vectors.current.toLocaleString(tag),
              total: config.vectors.total.toLocaleString(tag),
            })}
          </CardDescription>
        </div>
        {p.running ? (
          <Badge variant="secondary">
            <Loader2 data-icon="inline-start" className="animate-spin" />
            {t("embedding.reembed.running")}
          </Badge>
        ) : upToDate ? (
          <Badge variant="outline" className="text-green-700 dark:text-green-400">
            <CheckCircle2 data-icon="inline-start" />
            {t("embedding.reembed.upToDate")}
          </Badge>
        ) : (
          <Badge variant="outline">{t("embedding.reembed.pendingBadge", { count: stale.toLocaleString(tag) })}</Badge>
        )}
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {p.running || p.total > 0 ? (
          <div className="flex flex-col gap-1.5" data-testid="reembed-progress">
            <div
              role="progressbar"
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={pct}
              className="h-2 w-full overflow-hidden rounded-full bg-muted"
            >
              <div className="h-full bg-primary transition-all" style={{ width: `${pct}%` }} />
            </div>
            <p className="text-sm text-muted-foreground">
              {(p.done + p.skipped).toLocaleString(tag)} / {p.total.toLocaleString(tag)} ({pct}%)
              {p.skipped > 0 ? ` · ${t("embedding.reembed.skipped", { count: p.skipped.toLocaleString(tag) })}` : ""}
              {p.errors > 0 ? ` · ${t("embedding.reembed.errors", { count: p.errors })}` : ""}
              {p.running ? ` · ${t("embedding.reembed.eta", { eta: formatEta(p.eta_seconds) })}` : ""}
            </p>
          </div>
        ) : null}

        {p.last_error ? (
          <Alert variant="destructive">
            <AlertTitle>{t("embedding.reembed.lastError")}</AlertTitle>
            <AlertDescription>{p.last_error}</AlertDescription>
          </Alert>
        ) : null}

        {upToDate ? (
          <p className="text-sm text-muted-foreground">{t("embedding.reembed.allUpToDate")}</p>
        ) : (
          <p className="text-sm text-muted-foreground">
            {t("embedding.reembed.explain")}
          </p>
        )}

        <div className="flex gap-2">
          {p.running ? (
            <Button variant="destructive" size="sm" disabled={cancel.isPending} onClick={() => cancel.mutate()}>
              <Square data-icon="inline-start" />
              {t("embedding.reembed.cancel")}
            </Button>
          ) : (
            <Button size="sm" disabled={upToDate || !config.active || estimate.isPending || start.isPending} onClick={askStart}>
              <Play data-icon="inline-start" />
              {t("embedding.reembed.start")}
            </Button>
          )}
        </div>
        {!config.active ? (
          <p className="text-xs text-muted-foreground">{t("embedding.reembed.inactive")}</p>
        ) : null}
        {start.isError ? <p className="text-sm text-destructive">{start.error.message}</p> : null}
        {cancel.isError ? <p className="text-sm text-destructive">{cancel.error.message}</p> : null}
      </CardContent>

      <AlertDialog open={confirmStart !== null} onOpenChange={(o) => (!o ? setConfirmStart(null) : undefined)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("embedding.reembed.confirmTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("embedding.reembed.confirmDescription")}</AlertDialogDescription>
          </AlertDialogHeader>
          {confirmStart ? <EstimateSummary est={confirmStart} /> : null}
          <AlertDialogFooter>
            <AlertDialogCancel>{t("embedding.confirm.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                setConfirmStart(null);
                start.mutate();
              }}
            >
              {t("embedding.reembed.confirmStart")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  );
}

export default function EmbeddingPage() {
  const { t } = useT();
  const { data, isLoading, isError, error } = useEmbeddingConfig();

  if (isLoading) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-64" />
        <Skeleton className="h-40" />
      </div>
    );
  }
  if (isError || !data) {
    return <p className="text-sm text-destructive">{t("embedding.page.loadFailed", { message: error?.message ?? t("embedding.page.unexpected") })}</p>;
  }

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title={t("embedding.page.title")}
        subtitle={t("embedding.page.description", {
          source: data.source,
          state: data.active ? t("embedding.page.active") : t("embedding.page.inactive"),
        })}
        help="embedding"
        term="embedding"
      />
      <ConfigForm key={`${data.updated_at ?? ""}-${data.model}-${data.dimensions}-${data.base_url}`} config={data} />
      <ReembedCard config={data} />
    </div>
  );
}
