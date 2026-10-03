// Login screen (D13, item 2): paste the API key once, validate it against
// the real server (GET /stats — there is no "who am I" endpoint, see the
// comment in lib/api.ts) before accepting, store it in localStorage.
import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { KeyRound, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { validateApiKey } from "@/lib/api";
import { setApiKey } from "@/lib/auth";

export default function LoginPage() {
  const navigate = useNavigate();
  const [key, setKey] = useState("");
  const [status, setStatus] = useState<"idle" | "checking" | "invalid">("idle");

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!key.trim()) return;
    setStatus("checking");
    const ok = await validateApiKey(key.trim());
    if (!ok) {
      setStatus("invalid");
      return;
    }
    setApiKey(key.trim());
    navigate({ to: "/search" });
  }

  return (
    <div className="flex min-h-svh items-center justify-center bg-background px-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>M3m0s</CardTitle>
          <CardDescription>
            Paste the <code className="font-mono">m3m0s serve</code> API key to sign in.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit}>
            <FieldGroup>
              <Field data-invalid={status === "invalid" || undefined}>
                <FieldLabel htmlFor="api-key">API key</FieldLabel>
                <Input
                  id="api-key"
                  type="password"
                  autoFocus
                  autoComplete="off"
                  placeholder="mk_..."
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
                {status === "checking" ? (
                  <Loader2 data-icon="inline-start" className="animate-spin" />
                ) : (
                  <KeyRound data-icon="inline-start" />
                )}
                Sign in
              </Button>
            </FieldGroup>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
