// "Connect your agent": the server URL, an API key (minted here via POST /keys,
// shown once, or the first one from the server log) and ready-to-paste setups
// for Claude Code, pi and opencode.
import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { useMutation } from "@tanstack/react-query";
import { Eye, KeyRound, Loader2 } from "lucide-react";
import { createApiKey } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { CopyButton } from "@/components/ui/copy-button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { HelpTip } from "@/components/help/HelpTip";
import { PageHeader } from "@/components/layout/PageHeader";
import { useT } from "@/i18n";

type Tool = "claude" | "pi" | "opencode";

function CodeBlock({ code }: { code: string }) {
  return (
    <div className="flex flex-col gap-2">
      <pre className="overflow-x-auto rounded-md bg-muted p-3 font-mono text-xs leading-relaxed">{code}</pre>
      <div>
        <CopyButton text={code} />
      </div>
    </div>
  );
}

function snippets(url: string, key: string): Record<Tool, string> {
  return {
    claude: `claude mcp add m3m0s --env M3M0S_URL=${url} --env M3M0S_API_KEY=${key} -- m3m0s mcp`,
    pi: JSON.stringify({ m3m0s: { command: "m3m0s", args: ["mcp"], env: { M3M0S_URL: url, M3M0S_API_KEY: key } } }, null, 2),
    opencode: JSON.stringify(
      {
        mcp: {
          m3m0s: {
            type: "local",
            enabled: true,
            command: ["m3m0s", "mcp"],
            environment: { M3M0S_URL: url, M3M0S_API_KEY: key },
          },
        },
      },
      null,
      2,
    ),
  };
}

export default function ConnectPage() {
  const { t } = useT();
  const [tool, setTool] = useState<Tool>("claude");
  const url = typeof window !== "undefined" ? window.location.origin : "";
  const placeholder = t("connect.placeholder");
  // The raw key lives in component state only: it is shown once and never stored.
  const [newKey, setNewKey] = useState<string | null>(null);
  const createKey = useMutation({
    mutationFn: () => createApiKey("webui"),
    onSuccess: (res) => setNewKey(res.key),
  });
  const code = snippets(url, newKey ?? placeholder);

  return (
    <div className="flex flex-col gap-4">
      <PageHeader title={t("connect.title")} subtitle={t("connect.subtitle")} help="connect" />

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("connect.step1.title")}</CardTitle>
          <CardDescription>{t("connect.step1.body")}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-2">
          <code className="rounded-md bg-muted px-3 py-2 font-mono text-sm break-all">{url}</code>
          <div>
            <CopyButton text={url} />
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-1 text-base">
            {t("connect.step2.title")}
            <HelpTip term="apiKey" />
          </CardTitle>
          <CardDescription>{t("connect.step2.body")}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3 text-sm">
          {newKey ? (
            <div className="flex flex-col gap-2" role="status">
              <p className="font-medium">{t("connect.key.created")}</p>
              <CodeBlock code={newKey} />
              <p className="text-muted-foreground">{t("connect.key.once")}</p>
            </div>
          ) : (
            <div className="flex flex-col gap-2">
              <div>
                <Button type="button" size="sm" onClick={() => createKey.mutate()} disabled={createKey.isPending}>
                  {createKey.isPending ? (
                    <Loader2 data-icon="inline-start" className="animate-spin" />
                  ) : (
                    <KeyRound data-icon="inline-start" />
                  )}
                  {t("connect.key.create")}
                </Button>
              </div>
              {createKey.isError && (
                <p className="text-destructive">{t("connect.key.failed", { message: createKey.error.message })}</p>
              )}
            </div>
          )}
          <details>
            <summary className="cursor-pointer text-muted-foreground">{t("connect.key.existing")}</summary>
            <div className="mt-2 flex flex-col gap-2">
              <p>{t("connect.step2.howto", { placeholder })}</p>
              <CodeBlock code="docker compose logs | grep m3m0s_" />
            </div>
          </details>
          <p className="text-muted-foreground">{t("connect.step2.warn")}</p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("connect.step3.title")}</CardTitle>
          <CardDescription>{t("connect.step3.body")}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <Tabs value={tool} onValueChange={(v) => setTool(v as Tool)}>
            <TabsList>
              <TabsTrigger value="claude">{t("connect.tool.claude")}</TabsTrigger>
              <TabsTrigger value="pi">{t("connect.tool.pi")}</TabsTrigger>
              <TabsTrigger value="opencode">{t("connect.tool.opencode")}</TabsTrigger>
            </TabsList>
            {(["claude", "pi", "opencode"] as const).map((id) => (
              <TabsContent key={id} value={id} className="flex flex-col gap-2 pt-3">
                <p className="text-sm">{t(`connect.snippet.${id}`)}</p>
                <CodeBlock code={code[id]} />
              </TabsContent>
            ))}
          </Tabs>
          <p className="text-xs text-muted-foreground">{t("connect.requires")}</p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("connect.test.title")}</CardTitle>
          <CardDescription>{t("connect.test.body")}</CardDescription>
        </CardHeader>
        <CardContent>
          <Button variant="outline" size="sm" render={<Link to="/preview" />}>
            <Eye data-icon="inline-start" />
            {t("connect.test.action")}
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}
