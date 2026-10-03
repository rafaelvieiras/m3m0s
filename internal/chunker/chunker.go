// Package chunker splits a Markdown file into individually searchable
// pieces, per D11 (docs/decisions/) and the "Watch" section of
// docs/api-contract.md: 1 level-1 or level-2 header (# / ##) = 1 chunk = 1
// memory. Level 3+ headers stay inside the parent section's chunk.
package chunker

import "strings"

// minChunkableChars is the size floor (content already trimmed) below
// which it is not worth fragmenting the file, even if it has headers — it
// becomes a single chunk with the whole file.
const minChunkableChars = 300

// Chunk is a searchable piece of a file observed by m3m0s watch.
// Heading is empty when the chunk is the whole file (fallback) or is the
// preamble before the first header.
type Chunk struct {
	Heading string
	Content string
}

// Split divides content into chunks by level 1/2 Markdown header. A file
// with no level 1/2 headers, or with fewer than minChunkableChars
// characters (trimmed), always becomes a single chunk with the whole
// content.
func Split(content string) []Chunk {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil
	}

	lines := strings.Split(content, "\n")
	headerLines := headerLineIndexes(lines)

	if len(headerLines) == 0 || len(trimmed) < minChunkableChars {
		return []Chunk{{Heading: "", Content: content}}
	}

	var chunks []Chunk

	if preamble := strings.Join(lines[:headerLines[0]], "\n"); strings.TrimSpace(preamble) != "" {
		chunks = append(chunks, Chunk{Heading: "", Content: preamble})
	}

	for i, start := range headerLines {
		end := len(lines)
		if i+1 < len(headerLines) {
			end = headerLines[i+1]
		}
		section := strings.Join(lines[start:end], "\n")
		chunks = append(chunks, Chunk{Heading: headingText(lines[start]), Content: section})
	}

	return chunks
}

// headerLineIndexes returns the indexes of lines that are level 1 ("# ")
// or level 2 ("## ") headers — level 3+ ("### ") never matches either
// prefix (the third character differs from " " in both tests), so it is
// left out without needing an extra check.
func headerLineIndexes(lines []string) []int {
	var idx []int
	for i, line := range lines {
		if strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "## ") {
			idx = append(idx, i)
		}
	}
	return idx
}

func headingText(headerLine string) string {
	text := strings.TrimLeft(headerLine, "#")
	return strings.TrimSpace(text)
}
