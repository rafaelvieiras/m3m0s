// Shared Markdown renderer — used for the floating chat assistant's replies
// (D23) and for metadata.source_content (D13), which now often contains real
// excerpts of .md files indexed by `m3m0s watch` (D11/D24). `react-markdown`
// never renders raw HTML embedded in the content by default (without the
// rehype-raw plugin, which we deliberately do not add here) — the content may
// have gone through an LLM or come from a third-party file, so this is the
// XSS-safe behavior.
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { cn } from "@/lib/utils";

const components: Components = {
  p: ({ className, ...props }) => (
    <p className={cn("mb-2 last:mb-0", className)} {...props} />
  ),
  a: ({ className, ...props }) => (
    <a
      className={cn("font-medium text-primary underline underline-offset-2", className)}
      target="_blank"
      rel="noopener noreferrer"
      {...props}
    />
  ),
  ul: ({ className, ...props }) => (
    <ul className={cn("mb-2 list-disc space-y-0.5 pl-5 last:mb-0", className)} {...props} />
  ),
  ol: ({ className, ...props }) => (
    <ol className={cn("mb-2 list-decimal space-y-0.5 pl-5 last:mb-0", className)} {...props} />
  ),
  li: ({ className, ...props }) => <li className={cn("marker:text-muted-foreground", className)} {...props} />,
  h1: ({ className, ...props }) => (
    <h1 className={cn("mt-3 mb-1.5 text-base font-semibold first:mt-0", className)} {...props} />
  ),
  h2: ({ className, ...props }) => (
    <h2 className={cn("mt-3 mb-1.5 text-sm font-semibold first:mt-0", className)} {...props} />
  ),
  h3: ({ className, ...props }) => (
    <h3 className={cn("mt-2 mb-1 text-sm font-semibold first:mt-0", className)} {...props} />
  ),
  h4: ({ className, ...props }) => (
    <h4 className={cn("mt-2 mb-1 text-sm font-semibold first:mt-0", className)} {...props} />
  ),
  strong: ({ className, ...props }) => <strong className={cn("font-semibold", className)} {...props} />,
  em: ({ className, ...props }) => <em className={cn("italic", className)} {...props} />,
  code: ({ className, ...props }) => (
    <code
      className={cn(
        "rounded bg-foreground/10 px-1 py-0.5 font-mono text-[0.85em]",
        className,
      )}
      {...props}
    />
  ),
  pre: ({ className, ...props }) => (
    <pre
      className={cn(
        "mb-2 overflow-x-auto rounded-md bg-foreground/10 p-2 font-mono text-xs last:mb-0 [&_code]:bg-transparent [&_code]:p-0",
        className,
      )}
      {...props}
    />
  ),
  blockquote: ({ className, ...props }) => (
    <blockquote
      className={cn("mb-2 border-l-2 border-border pl-3 text-muted-foreground italic last:mb-0", className)}
      {...props}
    />
  ),
  hr: ({ className, ...props }) => <hr className={cn("my-3 border-border", className)} {...props} />,
  table: ({ className, ...props }) => (
    <div className="mb-2 overflow-x-auto last:mb-0">
      <table className={cn("w-full border-collapse text-xs", className)} {...props} />
    </div>
  ),
  th: ({ className, ...props }) => (
    <th className={cn("border-b border-border px-2 py-1 text-left font-medium", className)} {...props} />
  ),
  td: ({ className, ...props }) => <td className={cn("border-b border-border/50 px-2 py-1", className)} {...props} />,
};

export function Markdown({ content, className }: { content: string; className?: string }) {
  return (
    <div className={cn("text-sm leading-relaxed break-words", className)}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {content}
      </ReactMarkdown>
    </div>
  );
}
