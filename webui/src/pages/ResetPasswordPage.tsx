// Password reset / invite page (D35): reached from the single-use link that
// `m3m0s user reset|add` prints. It checks the link, asks for the new password
// twice and, on success, sends the owner to sign in (every session of the
// account was revoked server-side).
import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { CheckCircle2, KeyRound, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { FieldError, FieldGroup } from "@/components/ui/field";
import { PasswordFields, passwordProblemCode, passwordProblemText } from "@/components/auth/PasswordFields";
import { LanguageSwitcher } from "@/i18n/LanguageSwitcher";
import { useT } from "@/i18n";
import { useResetInfo, useResetPassword } from "@/hooks/useAuth";
import { ApiError } from "@/lib/api";
import { Route } from "@/routes/reset-password";

function Shell({ title, description, children }: { title: string; description?: string; children: React.ReactNode }) {
  return (
    <main className="relative flex min-h-svh items-center justify-center bg-background px-4">
      <LanguageSwitcher className="absolute top-4 right-4" />
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle as="h1">{title}</CardTitle>
          {description && <CardDescription>{description}</CardDescription>}
        </CardHeader>
        <CardContent>{children}</CardContent>
      </Card>
    </main>
  );
}

export default function ResetPasswordPage() {
  const { t } = useT();
  const { token } = Route.useSearch();
  const info = useResetInfo(token);
  const reset = useResetPassword();
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [touched, setTouched] = useState(false);
  const problem = passwordProblemCode(password, confirm);

  if (reset.isSuccess) {
    return (
      <Shell title={t("auth.reset.doneTitle")} description={t("auth.reset.doneDescription")}>
        <Button render={<Link to="/login" />} className="w-full">
          <CheckCircle2 data-icon="inline-start" />
          {t("auth.reset.goToSignIn")}
        </Button>
      </Shell>
    );
  }

  if (!token || info.isError) {
    const gone = !token || (info.error instanceof ApiError && info.error.status === 404);
    return (
      <Shell
        title={gone ? t("auth.reset.invalidTitle") : t("auth.reset.checkFailedTitle")}
        description={gone ? t("auth.reset.invalidDescription") : t("auth.reset.checkFailedDescription")}
      >
        <Button render={<Link to="/login" />} variant="outline" className="w-full">
          {t("auth.reset.backToSignIn")}
        </Button>
      </Shell>
    );
  }

  if (info.isLoading || !info.data) {
    return (
      <div className="flex min-h-svh items-center justify-center">
        <Loader2 className="size-5 animate-spin text-muted-foreground" />
      </div>
    );
  }

  const invite = info.data.kind === "invite";

  function submit(e: React.FormEvent) {
    e.preventDefault();
    setTouched(true);
    if (problem) return;
    reset.mutate({ token, password, confirm });
  }

  return (
    <Shell
      title={invite ? t("auth.reset.inviteTitle") : t("auth.reset.resetTitle")}
      description={
        invite
          ? t("auth.reset.inviteDescription", { username: info.data.username })
          : t("auth.reset.resetDescription", { username: info.data.username })
      }
    >
      <form onSubmit={submit}>
        <FieldGroup>
          <PasswordFields
            password={password}
            confirm={confirm}
            onPassword={setPassword}
            onConfirm={setConfirm}
            touched={touched}
          />
          {touched && problem && problem !== "mismatch" && <FieldError errors={[{ message: passwordProblemText(problem) }]} />}
          {reset.isError && (
            <FieldError
              errors={[{ message: reset.error instanceof ApiError ? reset.error.message : t("auth.unreachable") }]}
            />
          )}
          <Button type="submit" disabled={reset.isPending}>
            {reset.isPending ? (
              <Loader2 data-icon="inline-start" className="animate-spin" />
            ) : (
              <KeyRound data-icon="inline-start" />
            )}
            {invite ? t("auth.reset.setPassword") : t("auth.reset.resetPassword")}
          </Button>
        </FieldGroup>
      </form>
    </Shell>
  );
}
