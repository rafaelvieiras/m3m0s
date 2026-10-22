---
title: Recall and tuning
description: How per-prompt recall decides what to inject, which knobs exist, and what to check when a memory shows up (or doesn't).
section: Reference
order: 11
---

Recall runs on every non-trivial prompt. It is meant to be quiet: when nothing is clearly relevant, nothing is injected.

## What happens on each prompt

1. Prompts under 20 characters and slash commands are skipped without any call.
2. The prompt is embedded and searched in **vector** mode. Only vector search gives a cosine score that can be thresholded.
3. Hits must reach a cosine score of **0.60**. At most **3** entries and about **500** estimated tokens are kept.
4. A topic gate drops hits that share no term with the prompt. Misspelled names are matched fuzzily, and query terms are corrected server-side.
5. A hit whose domain is **another project** (`code:<other-folder>`) must clear the floor plus 0.10, so a loosely related note from an unrelated repository does not leak in. Pinned non-code domains, such as the one the Hermes provider uses, keep the plain floor.
6. Memories already injected in the session are remembered and not repeated.

Raw fallback entries are never injected. The call has a 4 second timeout, normally well under one second, and every failure is silent.

Clients that append the block themselves (pi, opencode) fence it as recalled memory with a note that it is not user input, as Hermes does.

## Tuning

| Variable | Effect |
|---|---|
| `M3M0S_PROMPT_INJECT=off` | Turns per-prompt recall off. Capture keeps working. |
| `M3M0S_PROMPT_MIN_SCORE` | Cosine floor, default `0.60`. Raise it for fewer, safer hits. |
| `M3M0S_PROMPT_FOREIGN_BOOST=off` | Removes the extra 0.10 for memories of other projects. |

Set them in the environment of the tool that launches the hook.

## Is it working?

Every injection is logged in `~/.m3m0s/hooks/claude-code.log`, with the score of each hit. In one production sample, relevant prompts scored 0.66 to 0.68 at the top and unrelated ones peaked around 0.40, so 0.60 separates them with little margin. If you see noise, raise the floor a little; if useful memories never appear, lower it a little and watch the log.

A prompt such as "go ahead" has no topic of its own, so it will not recall anything. Recall does not rewrite queries from the conversation.

## Domains

Captured memories are filed under a **domain**, by default `code:<folder name>` for the directory where the session started. Domain, not project, is the real scope: it is what recall uses to tell your own project from another one, and what you pass to `--domain` in the CLI. Set `M3M0S_HOOK_DOMAIN` to override it for a tool.
