// Human-readable picker for a memory's kind. `allowAuto` adds a first option
// ("Let M3m0s choose") whose value is the empty string, for the Add form where
// the server classifies on its own.
import { useId } from "react";
import { useT } from "@/i18n";
import { HelpTip } from "@/components/help/HelpTip";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { KINDS, isKind, kindDescKey, kindLabelKey } from "@/lib/kinds";

interface KindSelectProps {
  value: string;
  onChange: (value: string) => void;
  allowAuto?: boolean;
  id?: string;
}

export function KindSelect({ value, onChange, allowAuto = false, id }: KindSelectProps) {
  const { t } = useT();
  const autoId = useId();
  const selectId = id ?? autoId;
  const description = isKind(value) ? t(kindDescKey(value)) : allowAuto ? t("kinds.autoDesc") : "";
  return (
    <Field>
      <FieldLabel htmlFor={selectId}>
        {t("kinds.label")}
        <HelpTip term="kind" />
      </FieldLabel>
      <select
        id={selectId}
        className="h-8 w-full min-w-0 rounded-lg border border-input bg-transparent px-2.5 text-sm"
        value={value}
        onChange={(e) => onChange(e.target.value)}
      >
        {allowAuto && <option value="">{t("kinds.auto")}</option>}
        {KINDS.map((k) => (
          <option key={k} value={k}>
            {t(kindLabelKey(k))}
          </option>
        ))}
      </select>
      {description && <FieldDescription>{description}</FieldDescription>}
    </Field>
  );
}
