// Small text helpers for showing memory content in list rows.

/** Strips the most common Markdown syntax so a snippet reads as plain text. */
export function stripMarkdown(input: string): string {
  return input
    .replace(/```[a-z0-9_-]*\n?([\s\S]*?)```/gi, "$1") // fenced code blocks: keep the code
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1") // images: keep the alt text
    .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1") // links: keep the label
    .replace(/^\s{0,3}#{1,6}\s+/gm, "") // headings
    .replace(/^\s{0,3}>\s?/gm, "") // block quotes
    .replace(/^\s*[-*+]\s+/gm, "") // bullets
    .replace(/(\*\*|__)(.*?)\1/g, "$2") // bold
    .replace(/(\*|_)(.*?)\1/g, "$2") // italics
    .replace(/`([^`]*)`/g, "$1") // inline code
    .replace(/\s+/g, " ")
    .trim();
}

/** Plain-text snippet of at most `max` characters, cut with an ellipsis. */
export function snippet(input: string, max: number): string {
  const plain = stripMarkdown(input);
  return plain.length > max ? `${plain.slice(0, max).trimEnd()}…` : plain;
}
