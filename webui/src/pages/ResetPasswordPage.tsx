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
import { MISMATCH, PasswordFields, passwordProblem } from "@/components/auth/PasswordFields";
import { useResetInfo, useResetPassword } from "@/hooks/useAuth";
import { ApiError } from "@/lib/api";
import { Route } from "@/routes/reset-password";

function Shell({ title, description, children }: { title: string; description?: string; children: React.ReactNode }) {
  return (
    <div className="flex min-h-svh items-center justify-center bg-background px-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>{title}</CardTitle>
          {description && <CardDescription>{description}</CardDescription>}
        </CardHeader>
        <CardContent>{children}</CardContent>
      </Card>
    </div>
  );
}

export default function ResetPasswordPage() {
  const { token } = Route.useSearch();
  const info = useResetInfo(token);
  const reset = useResetPassword();
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [touched, setTouched] = useState(false);
  const problem = passwordProblem(password, confirm);

  if (reset.isSuccess) {
    return (
      <Shell title="Password updated" description="You were signed out everywhere. Sign in with the new password.">
        <Button render={<Link to="/login" />} className="w-full">
          <CheckCircle2 data-icon="inline-start" />
          Go to sign in
        </Button>
      </Shell>
    );
  }

  if (!token || info.isError) {
    const gone = !token || (info.error instanceof ApiError && info.error.status === 404);
    return (
      <Shell
        title={gone ? "This link is not valid" : "Could not check the link"}
        description={
          gone
            ? "It is expired, already used or incomplete. Ask for a new one with `m3m0s user reset <username>`."
            : "The server could not be reached. Try again in a moment."
        }
      >
        <Button render={<Link to="/login" />} variant="outline" className="w-full">
          Back to sign in
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
      title={invite ? "Choose your password" : "Set a new password"}
      description={
        invite
          ? `Finish creating the account “${info.data.username}”.`
          : `New password for “${info.data.username}”. Every session of this account will be signed out.`
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
          {touched && problem && problem !== MISMATCH && <FieldError errors={[{ message: problem }]} />}
          {reset.isError && (
            <FieldError
              errors={[{ message: reset.error instanceof ApiError ? reset.error.message : "Could not reach the server." }]}
            />
          )}
          <Button type="submit" disabled={reset.isPending}>
            {reset.isPending ? (
              <Loader2 data-icon="inline-start" className="animate-spin" />
            ) : (
              <KeyRound data-icon="inline-start" />
            )}
            {invite ? "Set password" : "Reset password"}
          </Button>
        </FieldGroup>
      </form>
    </Shell>
  );
}
