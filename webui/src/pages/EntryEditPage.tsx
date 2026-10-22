// Edit (PATCH /memories/:id, D13/api-contract.md) — TanStack Form + Zod,
// pre-populated with the entry's CURRENT values (including the full metadata,
// so metadata.source_content is not lost if the user only touches a tag —
// see docs/api-contract.md, section memory_update). Only sends in the PATCH
// the fields whose value actually changed relative to the pre-populated one
// — the contract's key-presence semantics (omitted field = leave untouched).
import { useState } from "react";
import { z } from "zod";
import { useForm } from "@tanstack/react-form";
import { Link } from "@tanstack/react-router";
import { Route } from "@/routes/entry-edit";
import { useT } from "@/i18n";
import { ArrowLeft, Loader2, Save } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { useEntry, useUpdateMemory } from "@/hooks/useMemoryApi";
import { domainSchema, metadataSchema, type Entry, type UpdatePayload } from "@/lib/schemas";

interface EditFormShape {
  content: string;
  domain: string;
  source: string;
  agentText: string;
  modelText: string;
  projectText: string;
  tagsText: string;
  metadataText: string;
}

function parseTags(tagsText: string): string[] {
  return tagsText
    .split(",")
    .map((t) => t.trim())
    .filter(Boolean);
}

export default function EntryEditPage() {
  const { t } = useT();
  const { id } = Route.useParams();
  const { data: entry, isError, error } = useEntry(id);

  // useForm needs the entry's current values to pre-populate — it only mounts
  // after the entry exists in the cache, so the form itself lives in a child
  // component (avoids a conditional hook: EntryEditPage does not call useForm
  // while entry is still undefined).
  if (isError || !entry) {
    return (
      <Empty className="border">
        <EmptyHeader>
          <EmptyTitle>{t("memory.notFound.title")}</EmptyTitle>
          <EmptyDescription>
            {error?.message ?? t("memory.notFound.hint")}
          </EmptyDescription>
        </EmptyHeader>
        <Button variant="outline" render={<Link to="/search" />}>
          <ArrowLeft data-icon="inline-start" />
          {t("memory.notFound.back")}
        </Button>
      </Empty>
    );
  }

  return <EditForm entry={entry} />;
}

function EditForm({ entry }: { entry: Entry }) {
  const { t } = useT();
  const { id } = Route.useParams();
  const navigate = Route.useNavigate();
  const updateMemory = useUpdateMemory(id);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const initial: EditFormShape = {
    content: entry.content,
    domain: entry.domain,
    source: entry.source,
    agentText: entry.agent ?? "",
    modelText: entry.model ?? "",
    projectText: entry.project ?? "",
    tagsText: entry.tags.join(","),
    metadataText: JSON.stringify(entry.metadata, null, 2),
  };

  const form = useForm({
    defaultValues: initial,
    onSubmit: async ({ value }) => {
      setSubmitError(null);
      const payload: UpdatePayload = {};

      if (value.content !== initial.content) payload.content = value.content;
      if (value.domain !== initial.domain) payload.domain = value.domain;
      if (value.source !== initial.source) payload.source = value.source;

      if (value.agentText !== initial.agentText) {
        payload.agent = value.agentText.trim() === "" ? null : value.agentText.trim();
      }

      if (value.modelText !== initial.modelText) {
        payload.model = value.modelText.trim() === "" ? null : value.modelText.trim();
      }

      if (value.projectText !== initial.projectText) {
        payload.project = value.projectText.trim() === "" ? null : value.projectText.trim();
      }

      if (value.tagsText !== initial.tagsText) {
        payload.tags = parseTags(value.tagsText);
      }

      if (value.metadataText !== initial.metadataText) {
        try {
          const parsed = JSON.parse(value.metadataText || "{}");
          const result = metadataSchema.safeParse(parsed);
          if (!result.success) {
            setSubmitError(t("memory.edit.metadataFlat"));
            return;
          }
          payload.metadata = result.data;
        } catch {
          setSubmitError(t("memory.edit.metadataJson"));
          return;
        }
      }

      if (Object.keys(payload).length === 0) {
        navigate({ to: "/entries/$id", params: { id } });
        return;
      }

      try {
        await updateMemory.mutateAsync(payload);
        navigate({ to: "/entries/$id", params: { id } });
      } catch (err) {
        setSubmitError(err instanceof Error ? err.message : t("memory.edit.saveFailed"));
      }
    },
  });

  return (
    <div className="flex flex-col gap-4">
      <Button variant="ghost" size="sm" render={<Link to="/entries/$id" params={{ id }} />} className="self-start">
        <ArrowLeft data-icon="inline-start" />
        {t("memory.edit.cancel")}
      </Button>

      <Card>
        <CardHeader>
          <CardTitle className="text-base font-mono text-sm">{t("memory.edit.editing", { id: entry.id })}</CardTitle>
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
                validators={{ onChange: z.string().min(1, t("memory.edit.contentRequired")) }}
              >
                {(field) => (
                  <Field data-invalid={field.state.meta.errors.length > 0 || undefined}>
                    <FieldLabel htmlFor={field.name}>{t("memory.edit.content")}</FieldLabel>
                    <Textarea
                      id={field.name}
                      rows={6}
                      value={field.state.value}
                      onBlur={field.handleBlur}
                      onChange={(e) => field.handleChange(e.target.value)}
                      aria-invalid={field.state.meta.errors.length > 0 || undefined}
                    />
                    <FieldError errors={field.state.meta.errors.map((m) => ({ message: String(m) }))} />
                  </Field>
                )}
              </form.Field>

              <div className="grid gap-4 sm:grid-cols-2">
                <form.Field name="domain" validators={{ onChange: domainSchema }}>
                  {(field) => (
                    <Field data-invalid={field.state.meta.errors.length > 0 || undefined}>
                      <FieldLabel htmlFor={field.name}>domain</FieldLabel>
                      <Input
                        id={field.name}
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onChange={(e) => field.handleChange(e.target.value)}
                        aria-invalid={field.state.meta.errors.length > 0 || undefined}
                      />
                      <FieldError errors={field.state.meta.errors.map((m) => ({ message: String(m) }))} />
                    </Field>
                  )}
                </form.Field>

                <form.Field
                  name="source"
                  validators={{
                    onChange: z.string().min(1, t("memory.edit.sourceEmpty")),
                  }}
                >
                  {(field) => (
                    <Field data-invalid={field.state.meta.errors.length > 0 || undefined}>
                      <FieldLabel htmlFor={field.name}>source</FieldLabel>
                      <Input
                        id={field.name}
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onChange={(e) => field.handleChange(e.target.value)}
                        aria-invalid={field.state.meta.errors.length > 0 || undefined}
                      />
                      <FieldError errors={field.state.meta.errors.map((m) => ({ message: String(m) }))} />
                    </Field>
                  )}
                </form.Field>

                <form.Field name="agentText">
                  {(field) => (
                    <Field>
                      <FieldLabel htmlFor={field.name}>{t("memory.edit.agent")}</FieldLabel>
                      <Input
                        id={field.name}
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onChange={(e) => field.handleChange(e.target.value)}
                      />
                    </Field>
                  )}
                </form.Field>

                <form.Field name="modelText">
                  {(field) => (
                    <Field>
                      <FieldLabel htmlFor={field.name}>{t("memory.edit.model")}</FieldLabel>
                      <Input
                        id={field.name}
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onChange={(e) => field.handleChange(e.target.value)}
                      />
                    </Field>
                  )}
                </form.Field>

                <form.Field name="projectText">
                  {(field) => (
                    <Field>
                      <FieldLabel htmlFor={field.name}>{t("memory.edit.project")}</FieldLabel>
                      <Input
                        id={field.name}
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onChange={(e) => field.handleChange(e.target.value)}
                      />
                    </Field>
                  )}
                </form.Field>

                <form.Field name="tagsText">
                  {(field) => (
                    <Field>
                      <FieldLabel htmlFor={field.name}>{t("memory.edit.tags")}</FieldLabel>
                      <Input
                        id={field.name}
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onChange={(e) => field.handleChange(e.target.value)}
                      />
                    </Field>
                  )}
                </form.Field>
              </div>

              <form.Field name="metadataText">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor={field.name}>{t("memory.edit.metadata")}</FieldLabel>
                    <Textarea
                      id={field.name}
                      rows={6}
                      className="font-mono text-xs"
                      value={field.state.value}
                      onBlur={field.handleBlur}
                      onChange={(e) => field.handleChange(e.target.value)}
                    />
                    <FieldDescription>
                      {t("memory.edit.metadataHelp")}
                    </FieldDescription>
                  </Field>
                )}
              </form.Field>

              {submitError && <p className="text-sm text-destructive">{submitError}</p>}

              <form.Subscribe selector={(state) => [state.canSubmit, state.isSubmitting] as const}>
                {([canSubmit, isSubmitting]) => (
                  <Button type="submit" disabled={!canSubmit || isSubmitting} className="self-start">
                    {isSubmitting ? (
                      <Loader2 data-icon="inline-start" className="animate-spin" />
                    ) : (
                      <Save data-icon="inline-start" />
                    )}
                    {t("memory.edit.save")}
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
