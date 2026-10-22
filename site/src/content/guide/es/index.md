---
title: Introducción
description: Qué es m3m0s, cómo encajan sus piezas y por dónde empezar.
section: Start
order: 1
---

m3m0s es una memoria de largo plazo autoalojada para agentes LLM. Un único binario de Go ejecuta un servidor que almacena lo que aprenden tus agentes y lo devuelve a cualquier herramienta mediante **MCP**, **CLI** o **HTTP**. También incluye una WebUI embebida para explorar, editar y conversar con tu memoria.

## Las piezas

| Pieza | Qué hace |
|---|---|
| **Servidor** (`m3m0s serve`) | API HTTP y WebUI embebida en un solo proceso. Almacenamiento en SQLite, embeddings desde cualquier endpoint compatible con OpenAI. |
| **Hooks y plugins** | Se ejecutan dentro de tu herramienta de agente. Capturan las transcripciones de las sesiones y recuperan las memorias relevantes antes de cada prompt. |
| **Puente MCP** (`m3m0s mcp`) | Un servidor MCP por stdio para que un agente pueda buscar en la memoria de forma explícita. Es un puente sin estado hacia un servidor en ejecución. |
| **CLI** (`m3m0s add`, `search`, `recent`, …) | Comandos puntuales que se comunican con el servidor. |
| **Watcher** (`m3m0s watch <dir>`) | Indexa `AGENTS.md`, `CLAUDE.md`, `MEMORY.md` y `docs/**/*.md` de un proyecto y los reindexa cuando cambian. |

## El ciclo de la memoria

1. **Captura.** Al final de un turno o sesión, la integración entrega la transcripción a `m3m0s hook claude-code`. Los secretos se redactan y, si configuraste un modelo de extracción, el texto se destila en hechos atómicos breves.
2. **Almacenamiento.** Los hechos se convierten en embeddings, se deduplican por similitud y se guardan con un *dominio* (por ejemplo `code:myproject`). Un hecho que contradice a uno anterior marca al anterior como expirado. Un barrido programado desvanece los hechos obsoletos y deja un período de gracia antes de eliminarlos.
3. **Recuperación.** En cada prompt, la integración busca en tu memoria usando el texto del prompt e inyecta los pocos hechos que superan un umbral de relevancia. Consulta [Recuperación y ajuste](/es/guide/recall/).

## Siguientes pasos

- ¿Eres nuevo? Sigue la [Guía de inicio rápido](/es/guide/quickstart/).
- ¿Vas a ponerlo en un servidor? Lee [Despliegue](/es/guide/deploy/) y [Configuración](/es/guide/configuration/).
- Para conectar una herramienta: [Claude Code](/es/guide/claude-code/), [pi](/es/guide/pi/), [opencode](/es/guide/opencode/), [Hermes](/es/guide/hermes/) o [cualquier cliente MCP](/es/guide/mcp/).

> m3m0s está en una etapa temprana, pero su autor lo usa a diario. La API aún no está congelada. Las decisiones de diseño quedan registradas en el repositorio, en `docs/decisions/`.
