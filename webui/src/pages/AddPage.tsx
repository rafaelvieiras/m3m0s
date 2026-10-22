// Add screen (POST /memories) — TanStack Form + Zod. The most important item
// of the Phase 5 scope for the maintainer (D13): after submit, it shows a
// clear preview (original text vs. extracted facts, or the 1 raw entry)
// BEFORE going back/closing the form — see ExtractionPreview.
import { useState } from "react";
import { useForm } from "@tanstack/react-form";
import { Loader2, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { ExtractionPreview } from "@/components/memory/ExtractionPreview";
import { useAddMemory } from "@/hooks/useMemoryApi";
import { domainSchema, metadataSchema, type AddResponse } from "@/lib/schemas";
import { z } from "zod";
import { useT } from "@/i18n";

interface AddFormShape {
  content: string;
  domain: string;
  source: string;
  agent: string;
  tagsText: string;
  metadataText: string;
}

const defaultValues: AddFormShape = {
  content: "",
  domain: "",
  source: "",
  agent: "",
  tagsText: "",
  metadataText: "",
};

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
  const addMemory = useAddMemory();
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

      const response = await addMemory.mutateAsync({
        content: value.content,
        domain: value.domain,
        source: value.source.trim() || undefined,
        agent: value.agent.trim() || undefined,
        tags,
        metadata,
      });
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
      <div>
        <h1 className="text-lg font-semibold">{t("add.title")}</h1>
        <p className="text-sm text-muted-foreground">
          {t("add.subtitle")}
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("add.newEntry")}</CardTitle>
        </CardHeader>
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
                  </Field>
                )}
              </form.Field>

              <div className="grid gap-4 sm:grid-cols-2">
                <form.Field
                  name="domain"
                  validators={{
                    onChange: domainSchema,
                  }}
                >
                  {(field) => (
                    <Field data-invalid={field.state.meta.errors.length > 0 || undefined}>
                      <FieldLabel htmlFor={field.name}>domain</FieldLabel>
                      <Input
                        id={field.name}
                        name={field.name}
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onChange={(e) => field.handleChange(e.target.value)}
                        aria-invalid={field.state.meta.errors.length > 0 || undefined}
                        placeholder="code:m3m0s"
                      />
                      <FieldDescription>{t("add.domainHelp")}</FieldDescription>
                      <FieldError errors={field.state.meta.errors.map((m) => ({ message: String(m) }))} />
                    </Field>
                  )}
                </form.Field>

                <form.Field name="source">
                  {(field) => (
                    <Field>
                      <FieldLabel htmlFor={field.name}>source</FieldLabel>
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
                    const result = metadataSchema.safeParse(parsed);
                    if (!result.success) {
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
                    <FieldDescription>
                      {t("add.metadataHelp")}
                    </FieldDescription>
                    <FieldError errors={field.state.meta.errors.map((m) => ({ message: String(m) }))} />
                  </Field>
                )}
              </form.Field>

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
            </FieldGroup>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
