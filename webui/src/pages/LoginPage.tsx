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
import { MISMATCH, PasswordFields, passwordProblem } from "@/components/auth/PasswordFields";
import { useAuthStatus, useLogin, useOnboard } from "@/hooks/useAuth";
import { ApiError, validateApiKey } from "@/lib/api";
import { setApiKey } from "@/lib/auth";
import { usernameRegex } from "@/lib/schemas";

function Shell({ title, description, children }: { title: string; description: string; children: React.ReactNode }) {
  return (
    <div className="flex min-h-svh items-center justify-center bg-background px-4">
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
  return "Could not reach the server.";
}

function OnboardingForm() {
  const navigate = useNavigate();
  const onboard = useOnboard();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [touched, setTouched] = useState(false);
  const userProblem = username.length > 0 && !usernameRegex.test(username.trim().toLowerCase());
  const problem = passwordProblem(password, confirm);

  function submit(e: React.FormEvent) {
    e.preventDefault();
    setTouched(true);
    if (problem || userProblem || !username.trim()) return;
    onboard.mutate(
      { username: username.trim(), password, confirm },
      { onSuccess: () => navigate({ to: "/search" }) },
    );
  }

  return (
    <Shell title="Welcome to M3m0s" description="Create the first account to finish the setup.">
      <form onSubmit={submit}>
        <FieldGroup>
          <Field data-invalid={userProblem || undefined}>
            <FieldLabel htmlFor="onboard-username">Username</FieldLabel>
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
                errors={[{ message: "3-32 characters: lowercase letters, digits, '.', '_' or '-'." }]}
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
          {touched && problem && problem !== MISMATCH && <FieldError errors={[{ message: problem }]} />}
          {onboard.isError && <FieldError errors={[{ message: errorText(onboard.error) }]} />}
          <Button type="submit" disabled={onboard.isPending}>
            <Spinner busy={onboard.isPending} icon={<UserPlus data-icon="inline-start" />} />
            Create account
          </Button>
        </FieldGroup>
      </form>
    </Shell>
  );
}

function PasswordLogin({ onUseKey }: { onUseKey: () => void }) {
  const navigate = useNavigate();
  const login = useLogin();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");

  function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!username.trim() || !password) return;
    login.mutate(
      { username: username.trim(), password },
      { onSuccess: () => navigate({ to: "/search" }) },
    );
  }

  return (
    <Shell title="M3m0s" description="Sign in to your memory.">
      <form onSubmit={submit}>
        <FieldGroup>
          <Field data-invalid={login.isError || undefined}>
            <FieldLabel htmlFor="login-username">Username</FieldLabel>
            <Input
              id="login-username"
              autoFocus
              autoComplete="username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
            />
          </Field>
          <Field data-invalid={login.isError || undefined}>
            <FieldLabel htmlFor="login-password">Password</FieldLabel>
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
            Sign in
          </Button>
          <Button type="button" variant="link" size="sm" onClick={onUseKey}>
            Use an API key instead
          </Button>
        </FieldGroup>
      </form>
    </Shell>
  );
}

function ApiKeyLogin({ onBack }: { onBack: () => void }) {
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
    navigate({ to: "/search" });
  }

  return (
    <Shell title="M3m0s" description="Paste an API key to sign in.">
      <form onSubmit={submit}>
        <FieldGroup>
          <Field data-invalid={status === "invalid" || undefined}>
            <FieldLabel htmlFor="api-key">API key</FieldLabel>
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
              <FieldError errors={[{ message: "Invalid key or server unreachable." }]} />
            )}
          </Field>
          <Button type="submit" disabled={status === "checking" || !key.trim()}>
            <Spinner busy={status === "checking"} icon={<KeyRound data-icon="inline-start" />} />
            Sign in
          </Button>
          <Button type="button" variant="link" size="sm" onClick={onBack}>
            Back to username and password
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
