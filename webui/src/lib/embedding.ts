// Pure helpers for the embedding settings page (D36), kept out of the
// component so they can be unit-tested.
import { translate } from "@/i18n";
import type { EmbeddingConfigPayload, ReembedEstimate } from "./schemas";

export interface EmbeddingFormState {
  enabled: boolean;
  baseUrl: string;
  model: string;
  dimensions: string; // free text; "" or "0" = provider default
  apiKey: string; // write-only; "" = keep the saved one
  clearKey: boolean;
}

// parseDimensions returns the number to send, or an error message.
export function parseDimensions(raw: string): { value: number; error?: string } {
  const t = raw.trim();
  if (t === "") return { value: 0 };
  if (!/^\d+$/.test(t)) return { value: 0, error: translate("embedding.lib.dimensionsInvalid") };
  return { value: Number(t) };
}

// buildPayload turns the form into the PUT body. The key is only included
// when the user typed one or asked to remove the saved one.
export function buildPayload(f: EmbeddingFormState, confirmReembed = false): EmbeddingConfigPayload | { error: string } {
  const dims = parseDimensions(f.dimensions);
  if (dims.error) return { error: dims.error };
  if (f.model.trim() === "") return { error: translate("embedding.lib.modelRequired") };
  const p: EmbeddingConfigPayload = {
    enabled: f.enabled,
    base_url: f.baseUrl.trim(),
    model: f.model.trim(),
    dimensions: dims.value,
  };
  if (f.clearKey) p.api_key = "";
  else if (f.apiKey !== "") p.api_key = f.apiKey.trim();
  if (confirmReembed) p.confirm_reembed = true;
  return p;
}

export function formatEta(seconds: number): string {
  if (seconds <= 0) return translate("embedding.lib.etaUnknown");
  if (seconds < 60) return `${seconds}s`;
  const m = Math.floor(seconds / 60);
  if (m < 60) return `${m}min ${seconds % 60}s`;
  return `${Math.floor(m / 60)}h ${m % 60}min`;
}

export function progressPercent(done: number, total: number): number {
  if (total <= 0) return 0;
  return Math.max(0, Math.min(100, Math.floor((done / total) * 100)));
}

export function formatCost(est: Pick<ReembedEstimate, "estimated_cost_usd" | "price_per_million_tokens_usd" | "prices_as_of">): string {
  if (est.estimated_cost_usd == null) return translate("embedding.lib.costUnknown");
  const c = est.estimated_cost_usd;
  const price =
    est.price_per_million_tokens_usd != null
      ? translate("embedding.lib.costPrice", { price: est.price_per_million_tokens_usd })
      : "";
  return translate("embedding.lib.cost", {
    cost: c < 0.01 ? c.toFixed(4) : c.toFixed(2),
    price,
    date: est.prices_as_of,
  });
}
