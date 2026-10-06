// Password + confirmation pair shared by onboarding and the reset page. The
// server enforces the same rules (length only, 10-128 characters, both
// fields equal); this only gives feedback before the round trip.
import { Field, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { MAX_PASSWORD_LENGTH, MIN_PASSWORD_LENGTH } from "@/lib/schemas";

// The mismatch message is also shown under the confirmation field, so pages
// do not repeat it at form level.
export const MISMATCH = "The two passwords do not match.";

export function passwordProblem(password: string, confirm: string): string | null {
  if (password.length < MIN_PASSWORD_LENGTH) {
    return `Use at least ${MIN_PASSWORD_LENGTH} characters.`;
  }
  if (password.length > MAX_PASSWORD_LENGTH) {
    return `Use at most ${MAX_PASSWORD_LENGTH} characters.`;
  }
  if (password !== confirm) return MISMATCH;
  return null;
}

interface Props {
  password: string;
  confirm: string;
  onPassword: (v: string) => void;
  onConfirm: (v: string) => void;
  touched: boolean;
}

export function PasswordFields({ password, confirm, onPassword, onConfirm, touched }: Props) {
  const mismatch = touched && confirm.length > 0 && password !== confirm;
  return (
    <>
      <Field>
        <FieldLabel htmlFor="new-password">New password</FieldLabel>
        <Input
          id="new-password"
          type="password"
          autoComplete="new-password"
          value={password}
          onChange={(e) => onPassword(e.target.value)}
        />
        <FieldDescription>At least {MIN_PASSWORD_LENGTH} characters.</FieldDescription>
      </Field>
      <Field data-invalid={mismatch || undefined}>
        <FieldLabel htmlFor="confirm-password">Confirm password</FieldLabel>
        <Input
          id="confirm-password"
          type="password"
          autoComplete="new-password"
          value={confirm}
          aria-invalid={mismatch || undefined}
          onChange={(e) => onConfirm(e.target.value)}
        />
        {mismatch && <FieldError errors={[{ message: MISMATCH }]} />}
      </Field>
    </>
  );
}
