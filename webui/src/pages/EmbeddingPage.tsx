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
  return (
    <ul className="list-disc pl-5 text-sm">
      <li>
        {est.pending.toLocaleString()} memories to re-embed with <code>{est.model}</code>
      </li>
      <li>
        ~{est.estimated_tokens.toLocaleString()} tokens ({est.characters.toLocaleString()} characters / 4 — an
        approximation, not what the provider will bill exactly)
      </li>
      <li>Cost: {formatCost(est)}</li>
    </ul>
  );
}

function ConfigForm({ config }: { config: EmbeddingConfig }) {
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
      toast.success(res.reembed_started ? "Saved — re-embedding started" : "Embedding configuration saved");
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

  const keyLabel = config.has_api_key ? `set (from ${config.key_source})` : "not set";

  return (
    <Card>
      <CardHeader>
        <CardTitle>Provider</CardTitle>
        <CardDescription>
          Any OpenAI-compatible embeddings endpoint. Saving makes a real test call first; a rejected change leaves the
          active configuration untouched.
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
              <FieldLabel htmlFor="emb-enabled">Vector search enabled</FieldLabel>
            </Field>
            <Field>
              <FieldLabel htmlFor="emb-base">Base URL</FieldLabel>
              <Input
                id="emb-base"
                value={form.baseUrl}
                placeholder="empty = server default (OpenAI)"
                onChange={(e) => set("baseUrl", e.target.value)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="emb-model">Model</FieldLabel>
              <Input id="emb-model" value={form.model} onChange={(e) => set("model", e.target.value)} />
            </Field>
            <Field>
              <FieldLabel htmlFor="emb-dims">Dimensions</FieldLabel>
              <Input
                id="emb-dims"
                inputMode="numeric"
                value={form.dimensions}
                placeholder="empty = model default"
                onChange={(e) => set("dimensions", e.target.value)}
              />
              <FieldDescription>
                Requested vector size{config.vector_dims > 0 ? ` (the provider currently returns ${config.vector_dims})` : ""}.
              </FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="emb-key">API key</FieldLabel>
              <Input
                id="emb-key"
                type="password"
                autoComplete="new-password"
                value={form.apiKey}
                disabled={form.clearKey}
                placeholder={config.has_api_key ? "leave empty to keep the current key" : "paste a key"}
                onChange={(e) => set("apiKey", e.target.value)}
              />
              <FieldDescription>
                Current key: {keyLabel}. The key is write-only — it is never shown again after saving.
              </FieldDescription>
              {config.key_source === "database" ? (
                <label className="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    className="size-4"
                    checked={form.clearKey}
                    onChange={(e) => set("clearKey", e.target.checked)}
                  />
                  Remove the saved key (the server environment key applies, if any)
                </label>
              ) : null}
            </Field>
          </FieldGroup>

          {error ? (
            <Alert variant="destructive">
              <AlertTitle>Not saved</AlertTitle>
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}

          <div>
            <Button type="submit" disabled={save.isPending || estimate.isPending}>
              {save.isPending ? <Loader2 data-icon="inline-start" className="animate-spin" /> : <Save data-icon="inline-start" />}
              Save
            </Button>
          </div>
        </form>
      </CardContent>

      <AlertDialog open={pending !== null} onOpenChange={(o) => (!o ? setPending(null) : undefined)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Re-embed stored memories?</AlertDialogTitle>
            <AlertDialogDescription>
              {pending
                ? `${pending.stale.toLocaleString()} of ${pending.total.toLocaleString()} memories would need a new vector for the new model or size. Until re-embedded they are found by keyword only.`
                : null}
            </AlertDialogDescription>
          </AlertDialogHeader>
          {pending ? <EstimateSummary est={pending.estimate} /> : null}
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              disabled={save.isPending}
              onClick={(e) => {
                e.preventDefault(); // keep the dialog open until the save settles
                if (pending) void submit({ ...pending.payload, confirm_reembed: true });
              }}
            >
              {save.isPending ? "Saving…" : "Save and re-embed"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  );
}

function ReembedCard({ config }: { config: EmbeddingConfig }) {
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
          <CardTitle>Re-embedding</CardTitle>
          <CardDescription>
            {config.vectors.current.toLocaleString()} of {config.vectors.total.toLocaleString()} memories use the active
            model and size.
          </CardDescription>
        </div>
        {p.running ? (
          <Badge variant="secondary">
            <Loader2 data-icon="inline-start" className="animate-spin" />
            Running
          </Badge>
        ) : upToDate ? (
          <Badge variant="outline" className="text-green-600 dark:text-green-400">
            <CheckCircle2 data-icon="inline-start" />
            Up to date
          </Badge>
        ) : (
          <Badge variant="outline">{stale.toLocaleString()} pending</Badge>
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
              {(p.done + p.skipped).toLocaleString()} / {p.total.toLocaleString()} ({pct}%)
              {p.skipped > 0 ? ` · ${p.skipped.toLocaleString()} already updated elsewhere` : ""}
              {p.errors > 0 ? ` · ${p.errors} errors` : ""}
              {p.running ? ` · ETA ${formatEta(p.eta_seconds)}` : ""}
            </p>
          </div>
        ) : null}

        {p.last_error ? (
          <Alert variant="destructive">
            <AlertTitle>Last run stopped with an error</AlertTitle>
            <AlertDescription>{p.last_error}</AlertDescription>
          </Alert>
        ) : null}

        {upToDate ? (
          <p className="text-sm text-muted-foreground">Everything is up to date — there is nothing to re-embed.</p>
        ) : (
          <p className="text-sm text-muted-foreground">
            Memories outside the active model/size are found by keyword only until re-embedded. Re-embedding spends
            provider tokens, so it never starts by itself; cancelling or restarting the server loses nothing — start
            again to continue.
          </p>
        )}

        <div className="flex gap-2">
          {p.running ? (
            <Button variant="destructive" size="sm" disabled={cancel.isPending} onClick={() => cancel.mutate()}>
              <Square data-icon="inline-start" />
              Cancel
            </Button>
          ) : (
            <Button size="sm" disabled={upToDate || !config.active || estimate.isPending || start.isPending} onClick={askStart}>
              <Play data-icon="inline-start" />
              Start re-embedding
            </Button>
          )}
        </div>
        {!config.active ? (
          <p className="text-xs text-muted-foreground">Embeddings are disabled or have no API key, so re-embedding cannot run.</p>
        ) : null}
        {start.isError ? <p className="text-sm text-destructive">{start.error.message}</p> : null}
        {cancel.isError ? <p className="text-sm text-destructive">{cancel.error.message}</p> : null}
      </CardContent>

      <AlertDialog open={confirmStart !== null} onOpenChange={(o) => (!o ? setConfirmStart(null) : undefined)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Start re-embedding?</AlertDialogTitle>
            <AlertDialogDescription>This sends the stored text to the embedding provider.</AlertDialogDescription>
          </AlertDialogHeader>
          {confirmStart ? <EstimateSummary est={confirmStart} /> : null}
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                setConfirmStart(null);
                start.mutate();
              }}
            >
              Start
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  );
}

export default function EmbeddingPage() {
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
    return <p className="text-sm text-destructive">Failed to load embedding settings: {error?.message ?? "unexpected response"}</p>;
  }

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h1 className="text-lg font-semibold">Embedding</h1>
        <p className="text-sm text-muted-foreground">
          Model, endpoint, dimensions and key used for vector search (D36). Configured via {data.source}; vector search
          is {data.active ? "active" : "inactive"}.
        </p>
      </div>
      <ConfigForm key={`${data.updated_at ?? ""}-${data.model}-${data.dimensions}-${data.base_url}`} config={data} />
      <ReembedCard config={data} />
    </div>
  );
}
