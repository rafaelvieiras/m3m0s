// Add screen (POST /memories) — TanStack Form + Zod. The simple form is one
// content field with clickable examples and a human type picker; origin, agent,
// tags and the raw extra data live under "Advanced options". After submit it
// shows a clear preview (original text vs. extracted facts, or the 1 raw
// entry) BEFORE going back/closing the form — see ExtractionPreview (D13).
import { useState } from "react";
import { useForm } from "@tanstack/react-form";
import { useQueryClient } from "@tanstack/react-query";
import { ChevronDown, Loader2, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { HelpTip } from "@/components/help/HelpTip";
import { PageHeader } from "@/components/layout/PageHeader";
import { ExtractionPreview } from "@/components/memory/ExtractionPreview";
import { KindSelect } from "@/components/memory/KindSelect";
import { useAddMemory } from "@/hooks/useMemoryApi";
import { updateMemory } from "@/lib/api";
import { useAdvancedMode } from "@/lib/mode";
import { domainRegex, metadataSchema, type AddResponse } from "@/lib/schemas";
import { z } from "zod";
import { useT, type TKey } from "@/i18n";

/** Topic used when the person leaves the field empty (the API requires one). */
export const DEFAULT_DOMAIN = "personal:general";

interface AddFormShape {
  content: string;
  kind: string;
  domain: string;
  project: string;
  source: string;
  agent: string;
  tagsText: string;
  metadataText: string;
}

const defaultValues: AddFormShape = {
  content: "",
  kind: "",
  domain: "",
  project: "",
  source: "",
  agent: "",
  tagsText: "",
  metadataText: "",
};

const EXAMPLES: { text: TKey; kind: string }[] = [
  { text: "add.example1", kind: "decision" },
  { text: "add.example2", kind: "preference" },
  { text: "add.example3", kind: "procedure" },
];

function parseTags(tagsText: string): string[] {
  return tagsText
    .split(",")
    .map((t) => t.trim())
    .filter(Boolean);
}

function parseMetadata(metadataText: string): Record<string, string> {
  const trimmed = metadataText.trim();
  if (!trimmed) return {};
  return JSON.parse(trimmed);
}

export default function AddPage() {
  const { t } = useT();
  const qc = useQueryClient();
  const addMemory = useAddMemory();
  const [advanced] = useAdvancedMode();
  const [result, setResult] = useState<{ content: string; response: AddResponse } | null>(null);

  const form = useForm({
    defaultValues,
    onSubmit: async ({ value }) => {
      const tags = parseTags(value.tagsText);
      let metadata: Record<string, string> = {};
      try {
        metadata = parseMetadata(value.metadataText);
      } catch {
        // already blocked by the field validator — should not get here.
        return;
      }

      let response = await addMemory.mutateAsync({
        content: value.content,
        domain: value.domain.trim() || DEFAULT_DOMAIN,
        source: value.source.trim() || undefined,
        agent: value.agent.trim() || undefined,
        project: value.project.trim() || undefined,
        tags,
        metadata,
      });

      // The add endpoint classifies on its own; a type picked by the person is
      // applied right after as a manual correction (the same PATCH the edit
      // screen uses).
      if (value.kind) {
        const updated = await Promise.all(
          response.entries.map(async (item) => {
            try {
              return { ...item, entry: await updateMemory(item.entry.id, { kind: value.kind }) };
            } catch {
              return item;
            }
          }),
        );
        response = { ...response, entries: updated };
        qc.invalidateQueries({ queryKey: ["recent"] });
      }
      setResult({ content: value.content, response });
    },
  });

  if (result) {
    return (
      <div className="flex flex-col gap-4">
        <h1 className="text-lg font-semibold">{t("add.added")}</h1>
        <ExtractionPreview
          originalContent={result.content}
          response={result.response}
          onAddAnother={() => {
            setResult(null);
            form.reset();
          }}
        />
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={t("add.title")} subtitle={t("add.subtitle")} help="add" />

      <Card>
        <CardContent>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              e.stopPropagation();
              form.handleSubmit();
            }}
          >
            <FieldGroup>
              <form.Field
                name="content"
                validators={{
                  onChange: z.string().min(1, t("add.contentRequired")),
                }}
              >
                {(field) => (
                  <Field data-invalid={field.state.meta.errors.length > 0 || undefined}>
                    <FieldLabel htmlFor={field.name}>{t("add.content")}</FieldLabel>
                    <Textarea
                      id={field.name}
                      name={field.name}
                      rows={6}
                      value={field.state.value}
                      onBlur={field.handleBlur}
                      onChange={(e) => field.handleChange(e.target.value)}
                      aria-invalid={field.state.meta.errors.length > 0 || undefined}
                      placeholder={t("add.contentPlaceholder")}
                    />
                    <FieldError errors={field.state.meta.errors.map((m) => ({ message: String(m) }))} />
                    <div className="flex flex-wrap items-center gap-2 pt-1">
                      <span className="text-xs text-muted-foreground">{t("add.examples")}</span>
                      {EXAMPLES.map((ex) => (
                        <Button
                          key={ex.text}
                          type="button"
                          size="xs"
                          variant="outline"
                          className="h-auto max-w-full py-1 text-left whitespace-normal"
                          onClick={() => {
                            field.handleChange(t(ex.text));
                            form.setFieldValue("kind", ex.kind);
                          }}
                        >
                          {t(ex.text)}
                        </Button>
                      ))}
                    </div>
                  </Field>
                )}
              </form.Field>

              <div className="grid gap-4 sm:grid-cols-2">
                <form.Field name="kind">
                  {(field) => (
                    <KindSelect id={field.name} value={field.state.value} onChange={field.handleChange} allowAuto />
                  )}
                </form.Field>

                <form.Field name="project">
                  {(field) => (
                    <Field>
                      <FieldLabel htmlFor={field.name}>
                        {t("common.field.project")} <span className="font-normal text-muted-foreground">({t("add.optional")})</span>
                        <HelpTip term="project" />
                      </FieldLabel>
                      <Input
                        id={field.name}
                        name={field.name}
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onChange={(e) => field.handleChange(e.target.value)}
                        placeholder="my-app"
                      />
                    </Field>
                  )}
                </form.Field>

                <form.Field
                  name="domain"
                  validators={{
                    onChange: ({ value }) =>
                      value.trim() === "" || domainRegex.test(value.trim()) ? undefined : t("error.invalid_domain"),
                  }}
                >
                  {(field) => (
                    <Field data-invalid={field.state.meta.errors.length > 0 || undefined}>
                      <FieldLabel htmlFor={field.name}>
                        {t("common.field.domain")} <span className="font-normal text-muted-foreground">({t("add.optional")})</span>
                        <HelpTip term="domain" />
                      </FieldLabel>
                      <Input
                        id={field.name}
                        name={field.name}
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onChange={(e) => field.handleChange(e.target.value)}
                        aria-invalid={field.state.meta.errors.length > 0 || undefined}
                        placeholder={DEFAULT_DOMAIN}
                      />
                      <FieldDescription>{t("add.domainHelp", { default: DEFAULT_DOMAIN })}</FieldDescription>
                      <FieldError errors={field.state.meta.errors.map((m) => ({ message: String(m) }))} />
                    </Field>
                  )}
                </form.Field>
              </div>

              <details className="group" open={advanced || undefined}>
                <summary className="inline-flex cursor-pointer list-none items-center gap-1 text-sm text-muted-foreground outline-none hover:text-foreground [&::-webkit-details-marker]:hidden">
                  <ChevronDown className="size-4 transition-transform group-open:rotate-180" aria-hidden />
                  {t("add.advancedOptions")}
                </summary>
                <div className="mt-3 flex flex-col gap-4">
                  <div className="grid gap-4 sm:grid-cols-2">
                    <form.Field name="source">
                      {(field) => (
                        <Field>
                          <FieldLabel htmlFor={field.name}>{t("common.field.source")}</FieldLabel>
                          <Input
                            id={field.name}
                            name={field.name}
                            value={field.state.value}
                            onBlur={field.handleBlur}
                            onChange={(e) => field.handleChange(e.target.value)}
                            placeholder={t("add.sourcePlaceholder")}
                          />
                        </Field>
                      )}
                    </form.Field>

                    <form.Field name="agent">
                      {(field) => (
                        <Field>
                          <FieldLabel htmlFor={field.name}>{t("add.agent")}</FieldLabel>
                          <Input
                            id={field.name}
                            name={field.name}
                            value={field.state.value}
                            onBlur={field.handleBlur}
                            onChange={(e) => field.handleChange(e.target.value)}
                            placeholder="my-agent"
                          />
                        </Field>
                      )}
                    </form.Field>

                    <form.Field name="tagsText">
                      {(field) => (
                        <Field>
                          <FieldLabel htmlFor={field.name}>{t("add.tags")}</FieldLabel>
                          <Input
                            id={field.name}
                            name={field.name}
                            value={field.state.value}
                            onBlur={field.handleBlur}
                            onChange={(e) => field.handleChange(e.target.value)}
                            placeholder="chapter-3,draft"
                          />
                        </Field>
                      )}
                    </form.Field>
                  </div>

                  <form.Field
                    name="metadataText"
                    validators={{
                      onChange: ({ value }) => {
                        if (!value.trim()) return undefined;
                        let parsed: unknown;
                        try {
                          parsed = JSON.parse(value);
                        } catch {
                          return t("add.metadataJson");
                        }
                        const parsedMetadata = metadataSchema.safeParse(parsed);
                        if (!parsedMetadata.success) {
                          return t("add.metadataFlat");
                        }
                        return undefined;
                      },
                    }}
                  >
                    {(field) => (
                      <Field data-invalid={field.state.meta.errors.length > 0 || undefined}>
                        <FieldLabel htmlFor={field.name}>{t("add.metadata")}</FieldLabel>
                        <Textarea
                          id={field.name}
                          name={field.name}
                          rows={3}
                          value={field.state.value}
                          onBlur={field.handleBlur}
                          onChange={(e) => field.handleChange(e.target.value)}
                          aria-invalid={field.state.meta.errors.length > 0 || undefined}
                          placeholder='{"conversationId": "conv_8f2a"}'
                        />
                        <FieldDescription>{t("add.metadataHelp")}</FieldDescription>
                        <FieldError errors={field.state.meta.errors.map((m) => ({ message: String(m) }))} />
                      </Field>
                    )}
                  </form.Field>
                </div>
              </details>

              <form.Subscribe selector={(state) => [state.canSubmit, state.isSubmitting] as const}>
                {([canSubmit, isSubmitting]) => (
                  <Button type="submit" disabled={!canSubmit || isSubmitting} className="self-start">
                    {isSubmitting ? (
                      <Loader2 data-icon="inline-start" className="animate-spin" />
                    ) : (
                      <Plus data-icon="inline-start" />
                    )}
                    {t("add.submit")}
                  </Button>
                )}
              </form.Subscribe>
              {addMemory.isError && <p className="text-sm text-destructive">{addMemory.error.message}</p>}
            </FieldGroup>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
