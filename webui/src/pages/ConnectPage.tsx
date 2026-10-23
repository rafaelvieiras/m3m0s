// "Connect your agent": the server URL, where the API key comes from, and
// ready-to-paste setups for Claude Code, pi and opencode. The server has no
// endpoint to create API keys, so the page explains where the first key is
// printed instead of pretending to mint one.
import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { Eye } from "lucide-react";
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
  const code = snippets(url, placeholder);

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
          <p>{t("connect.step2.howto", { placeholder })}</p>
          <CodeBlock code="docker compose logs | grep m3m0s_" />
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
