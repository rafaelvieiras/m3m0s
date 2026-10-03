// Post-submit preview of the Add screen (D13: "validate what is being
// written" — the most important item of the scope for the maintainer). Shown
// BEFORE closing the form/navigating elsewhere, see docs/implementation-plan.md
// Phase 5. extracted=true: original text side by side with the N atomic facts
// generated (D12); extracted=false: just the 1 entry created.
import { CheckCircle2, Copy, RefreshCw } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import type { AddResponse } from "@/lib/schemas";

interface ExtractionPreviewProps {
  originalContent: string;
  response: AddResponse;
  onAddAnother: () => void;
}

export function ExtractionPreview({
  originalContent,
  response,
  onAddAnother,
}: ExtractionPreviewProps) {
  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-2 rounded-lg border border-primary/30 bg-primary/5 px-4 py-3">
        <CheckCircle2 className="size-5 shrink-0 text-primary" />
        <div>
          <p className="text-sm font-medium">
            {response.extracted
              ? `Fact extraction on: ${response.entries.length} fact(s) generated from the text.`
              : "Fact extraction off — the text was saved as is."}
          </p>
        </div>
      </div>

      <div className={response.extracted ? "grid gap-4 md:grid-cols-2" : "grid gap-4"}>
        {response.extracted && (
          <Card>
            <CardHeader>
              <CardTitle className="text-sm text-muted-foreground">Original text</CardTitle>
            </CardHeader>
            <CardContent>
              <p className="whitespace-pre-wrap text-sm">{originalContent}</p>
            </CardContent>
          </Card>
        )}

        <div className="flex flex-col gap-3">
          {response.entries.map(({ entry, deduped }, i) => (
            <Card key={entry.id}>
              <CardHeader className="flex-row items-center justify-between gap-2 space-y-0">
                <CardTitle className="text-sm">
                  {response.extracted ? `Fact ${i + 1}` : "Entry created"}
                </CardTitle>
                <div className="flex items-center gap-1.5">
                  {deduped ? (
                    <Badge variant="secondary">
                      <Copy data-icon="inline-start" />
                      deduped (merged with existing)
                    </Badge>
                  ) : (
                    <Badge variant="outline">new</Badge>
                  )}
                </div>
              </CardHeader>
              <CardContent className="flex flex-col gap-2">
                <p className="whitespace-pre-wrap text-sm">{entry.content}</p>
                <div className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
                  <Badge variant="secondary">{entry.domain}</Badge>
                  <Badge variant="outline">{entry.source}</Badge>
                  {entry.agent && <Badge variant="outline">@{entry.agent}</Badge>}
                  <span className="font-mono">{entry.id}</span>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      </div>

      <div className="flex justify-end">
        <Button variant="outline" onClick={onAddAnother}>
          <RefreshCw data-icon="inline-start" />
          Add another memory
        </Button>
      </div>
    </div>
  );
}
