// Sign-in screen (D13 item 2, extended by D35). Three states, decided by the
// server: first-run onboarding (no account yet), username/password sign-in,
// and the API key paste kept as a break-glass path.
import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { KeyRound, Loader2, LogIn, UserPlus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { PasswordFields, passwordProblemCode, passwordProblemText } from "@/components/auth/PasswordFields";
import { LanguageSwitcher } from "@/i18n/LanguageSwitcher";
import { translate, useT } from "@/i18n";
import { useAuthStatus, useLogin, useOnboard } from "@/hooks/useAuth";
import { ApiError, validateApiKey } from "@/lib/api";
import { setApiKey } from "@/lib/auth";
import { usernameRegex } from "@/lib/schemas";

function Shell({ title, description, children }: { title: string; description: string; children: React.ReactNode }) {
  return (
    <div className="relative flex min-h-svh items-center justify-center bg-background px-4">
      <LanguageSwitcher className="absolute top-4 right-4" />
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>{title}</CardTitle>
          <CardDescription>{description}</CardDescription>
        </CardHeader>
        <CardContent>{children}</CardContent>
      </Card>
    </div>
  );
}

function Spinner({ busy, icon }: { busy: boolean; icon: React.ReactNode }) {
  return busy ? <Loader2 data-icon="inline-start" className="animate-spin" /> : icon;
}

function errorText(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  return translate("auth.unreachable");
}

function OnboardingForm() {
  const { t } = useT();
  const navigate = useNavigate();
  const onboard = useOnboard();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [touched, setTouched] = useState(false);
  const userProblem = username.length > 0 && !usernameRegex.test(username.trim().toLowerCase());
  const problem = passwordProblemCode(password, confirm);

  function submit(e: React.FormEvent) {
    e.preventDefault();
    setTouched(true);
    if (problem || userProblem || !username.trim()) return;
    onboard.mutate(
      { username: username.trim(), password, confirm },
      { onSuccess: () => navigate({ to: "/home" }) },
    );
  }

  return (
    <Shell title={t("auth.onboard.title")} description={t("auth.onboard.description")}>
      <div className="mb-4 rounded-md bg-muted/50 p-3 text-sm">
        <p className="font-medium">{t("auth.onboard.about")}</p>
        <ul className="mt-2 list-disc space-y-1 pl-4 text-muted-foreground">
          <li>{t("auth.onboard.use1")}</li>
          <li>{t("auth.onboard.use2")}</li>
          <li>{t("auth.onboard.use3")}</li>
        </ul>
      </div>
      <form onSubmit={submit}>
        <FieldGroup>
          <Field data-invalid={userProblem || undefined}>
            <FieldLabel htmlFor="onboard-username">{t("auth.username")}</FieldLabel>
            <Input
              id="onboard-username"
              autoComplete="username"
              autoFocus
              value={username}
              aria-invalid={userProblem || undefined}
              onChange={(e) => setUsername(e.target.value)}
            />
            {userProblem && (
              <FieldError
                errors={[{ message: t("auth.usernameRule") }]}
              />
            )}
          </Field>
          <PasswordFields
            password={password}
            confirm={confirm}
            onPassword={setPassword}
            onConfirm={setConfirm}
            touched={touched}
          />
          {touched && problem && problem !== "mismatch" && <FieldError errors={[{ message: passwordProblemText(problem) }]} />}
          {onboard.isError && <FieldError errors={[{ message: errorText(onboard.error) }]} />}
          <Button type="submit" disabled={onboard.isPending}>
            <Spinner busy={onboard.isPending} icon={<UserPlus data-icon="inline-start" />} />
            {t("auth.onboard.submit")}
          </Button>
        </FieldGroup>
      </form>
    </Shell>
  );
}

function PasswordLogin({ onUseKey }: { onUseKey: () => void }) {
  const { t } = useT();
  const navigate = useNavigate();
  const login = useLogin();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");

  function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!username.trim() || !password) return;
    login.mutate(
      { username: username.trim(), password },
      { onSuccess: () => navigate({ to: "/home" }) },
    );
  }

  return (
    <Shell title={t("auth.login.title")} description={t("auth.login.description")}>
      <form onSubmit={submit}>
        <FieldGroup>
          <Field data-invalid={login.isError || undefined}>
            <FieldLabel htmlFor="login-username">{t("auth.username")}</FieldLabel>
            <Input
              id="login-username"
              autoFocus
              autoComplete="username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
            />
          </Field>
          <Field data-invalid={login.isError || undefined}>
            <FieldLabel htmlFor="login-password">{t("auth.password")}</FieldLabel>
            <Input
              id="login-password"
              type="password"
              autoComplete="current-password"
              value={password}
              aria-invalid={login.isError || undefined}
              onChange={(e) => setPassword(e.target.value)}
            />
            {login.isError && <FieldError errors={[{ message: errorText(login.error) }]} />}
          </Field>
          <Button type="submit" disabled={login.isPending || !username.trim() || !password}>
            <Spinner busy={login.isPending} icon={<LogIn data-icon="inline-start" />} />
            {t("auth.login.submit")}
          </Button>
          <Button type="button" variant="link" size="sm" onClick={onUseKey}>
            {t("auth.login.useKey")}
          </Button>
        </FieldGroup>
      </form>
    </Shell>
  );
}

function ApiKeyLogin({ onBack }: { onBack: () => void }) {
  const { t } = useT();
  const navigate = useNavigate();
  const [key, setKey] = useState("");
  const [status, setStatus] = useState<"idle" | "checking" | "invalid">("idle");

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!key.trim()) return;
    setStatus("checking");
    if (!(await validateApiKey(key.trim()))) {
      setStatus("invalid");
      return;
    }
    setApiKey(key.trim());
    navigate({ to: "/home" });
  }

  return (
    <Shell title={t("auth.login.title")} description={t("auth.key.description")}>
      <form onSubmit={submit}>
        <FieldGroup>
          <Field data-invalid={status === "invalid" || undefined}>
            <FieldLabel htmlFor="api-key">{t("auth.key.label")}</FieldLabel>
            <Input
              id="api-key"
              type="password"
              autoFocus
              autoComplete="off"
              placeholder="m3m0s_..."
              value={key}
              aria-invalid={status === "invalid" || undefined}
              onChange={(e) => {
                setKey(e.target.value);
                if (status === "invalid") setStatus("idle");
              }}
            />
            {status === "invalid" && (
              <FieldError errors={[{ message: t("auth.key.invalid") }]} />
            )}
          </Field>
          <Button type="submit" disabled={status === "checking" || !key.trim()}>
            <Spinner busy={status === "checking"} icon={<KeyRound data-icon="inline-start" />} />
            {t("auth.login.submit")}
          </Button>
          <Button type="button" variant="link" size="sm" onClick={onBack}>
            {t("auth.key.back")}
          </Button>
        </FieldGroup>
      </form>
    </Shell>
  );
}

export default function LoginPage() {
  const status = useAuthStatus();
  const [useKey, setUseKey] = useState(false);

  if (status.isLoading) {
    return (
      <div className="flex min-h-svh items-center justify-center">
        <Loader2 className="size-5 animate-spin text-muted-foreground" />
      </div>
    );
  }
  // If the status cannot be read (server down), fall back to the API key form.
  if (status.isError || useKey) {
    return <ApiKeyLogin onBack={() => setUseKey(false)} />;
  }
  if (status.data?.needsOnboarding) return <OnboardingForm />;
  return <PasswordLogin onUseKey={() => setUseKey(true)} />;
}
