---
title: Introdução
description: O que é o m3m0s, como as peças se encaixam e por onde começar.
section: Start
order: 1
---

O m3m0s é uma memória de longo prazo self-hosted para agentes de LLM. Um único binário Go executa um servidor que armazena o que seus agentes aprendem e o devolve a qualquer ferramenta via **MCP**, **CLI** ou **HTTP**. Ele também embute uma WebUI para navegar, editar e conversar com a sua memória.

## As peças

| Peça | O que faz |
|---|---|
| **Servidor** (`m3m0s serve`) | API HTTP e WebUI embutida em um único processo. Armazenamento em SQLite, embeddings de qualquer endpoint compatível com a OpenAI. |
| **Hooks e plugins** | Rodam dentro da sua ferramenta de agente. Capturam as transcrições das sessões e recuperam memórias relevantes antes de cada prompt. |
| **Ponte MCP** (`m3m0s mcp`) | Um servidor MCP via stdio para que o agente possa buscar na memória de forma explícita. Uma ponte sem estado para um servidor em execução. |
| **CLI** (`m3m0s add`, `search`, `recent`, …) | Comandos pontuais que conversam com o servidor. |
| **Watcher** (`m3m0s watch <dir>`) | Indexa `AGENTS.md`, `CLAUDE.md`, `MEMORY.md` e `docs/**/*.md` de um projeto e reindexa quando mudam. |

## O ciclo da memória

1. **Captura.** No fim de um turno ou sessão, a integração entrega a transcrição para `m3m0s hook claude-code`. Segredos são ocultados e, se você configurou um modelo de extração, o texto é destilado em fatos atômicos curtos.
2. **Armazenamento.** Os fatos recebem embeddings, são deduplicados por similaridade e armazenados com um *domínio* (por exemplo, `code:myproject`). Um fato que contradiz um mais antigo marca o antigo como expirado. Uma varredura agendada esmaece fatos obsoletos e mantém um período de carência antes de apagar.
3. **Recuperação.** A cada prompt, a integração busca na sua memória pelo texto do prompt e injeta os poucos fatos que superam um limite de relevância. Veja [Recuperação e ajustes](/pt/guide/recall/).

## Para onde ir em seguida

- Novo por aqui? Execute o [Início rápido](/pt/guide/quickstart/).
- Vai colocar em um servidor? Leia [Deploy](/pt/guide/deploy/) e [Configuração](/pt/guide/configuration/).
- Conectando uma ferramenta: [Claude Code](/pt/guide/claude-code/), [pi](/pt/guide/pi/), [opencode](/pt/guide/opencode/), [Hermes](/pt/guide/hermes/) ou [qualquer cliente MCP](/pt/guide/mcp/).

> O m3m0s ainda é recente, mas já é usado diariamente pelo autor. A API ainda não está congelada. As decisões de design ficam registradas no repositório, em `docs/decisions/`.
