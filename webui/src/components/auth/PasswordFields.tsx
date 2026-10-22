// Password + confirmation pair shared by onboarding and the reset page. The
// server enforces the same rules (length only, 10-128 characters, both
// fields equal); this only gives feedback before the round trip.
import { Field, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { MAX_PASSWORD_LENGTH, MIN_PASSWORD_LENGTH } from "@/lib/schemas";
import { translate, useT } from "@/i18n";

export type PasswordProblemCode = "tooShort" | "tooLong" | "mismatch";

// The mismatch problem is also shown under the confirmation field, so pages
// do not repeat it at form level (they compare the code, not the text).
export function passwordProblemCode(password: string, confirm: string): PasswordProblemCode | null {
  if (password.length < MIN_PASSWORD_LENGTH) return "tooShort";
  if (password.length > MAX_PASSWORD_LENGTH) return "tooLong";
  if (password !== confirm) return "mismatch";
  return null;
}

export function passwordProblemText(code: PasswordProblemCode): string {
  switch (code) {
    case "tooShort":
      return translate("auth.pw.tooShort", { min: MIN_PASSWORD_LENGTH });
    case "tooLong":
      return translate("auth.pw.tooLong", { max: MAX_PASSWORD_LENGTH });
    case "mismatch":
      return translate("auth.pw.mismatch");
  }
}

export function passwordProblem(password: string, confirm: string): string | null {
  const code = passwordProblemCode(password, confirm);
  return code ? passwordProblemText(code) : null;
}

interface Props {
  password: string;
  confirm: string;
  onPassword: (v: string) => void;
  onConfirm: (v: string) => void;
  touched: boolean;
}

export function PasswordFields({ password, confirm, onPassword, onConfirm, touched }: Props) {
  const { t } = useT();
  const mismatch = touched && confirm.length > 0 && password !== confirm;
  return (
    <>
      <Field>
        <FieldLabel htmlFor="new-password">{t("auth.pw.new")}</FieldLabel>
        <Input
          id="new-password"
          type="password"
          autoComplete="new-password"
          value={password}
          onChange={(e) => onPassword(e.target.value)}
        />
        <FieldDescription>{t("auth.pw.hint", { min: MIN_PASSWORD_LENGTH })}</FieldDescription>
      </Field>
      <Field data-invalid={mismatch || undefined}>
        <FieldLabel htmlFor="confirm-password">{t("auth.pw.confirm")}</FieldLabel>
        <Input
          id="confirm-password"
          type="password"
          autoComplete="new-password"
          value={confirm}
          aria-invalid={mismatch || undefined}
          onChange={(e) => onConfirm(e.target.value)}
        />
        {mismatch && <FieldError errors={[{ message: t("auth.pw.mismatch") }]} />}
      </Field>
    </>
  );
}
