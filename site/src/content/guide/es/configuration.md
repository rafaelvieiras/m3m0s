---
title: Configuración
description: Variables de entorno del servidor y de los clientes.
section: Deploy
order: 4
---

El servidor se configura con variables de entorno (o con los flags equivalentes de `m3m0s serve`). Con Docker Compose, colócalas en `.env`.

## Servidor

| Variable | Valor por defecto | Propósito |
|---|---|---|
| `OPENAI_API_KEY` | ninguna (solo palabras clave) | Clave para los embeddings (`text-embedding-3-small`). Sin ella el servidor funciona solo con palabras clave: búsqueda de texto completo y deduplicación por coincidencia exacta. |
| `M3M0S_OPENAI_BASE_URL` | OpenAI | Endpoint alternativo de embeddings compatible con OpenAI. |
| `M3M0S_PORT` | `19234` | Puerto HTTP. |
| `M3M0S_DATA_DIR` | `~/.m3m0s/data` (`/data` en la imagen) | Dónde vive la base de datos SQLite. |
| `M3M0S_HOST_PORT` | `19234` | Puerto del host publicado por `compose.yaml`. |
| `M3M0S_ADD_SYNC_TIMEOUT` | `300ms` | Cuánto espera `add` antes de responder `202` y terminar en segundo plano. |
| `GOGC` | `50` | Objetivo del GC de Go. Valores más bajos cambian CPU por un pico de memoria menor. |
| `GOMEMLIMIT` | sin definir | Límite blando de memoria opcional, por ejemplo `600MiB`. |

## Extracción de hechos y chat (opcional)

La extracción convierte una entrada en varios hechos atómicos breves. Permanece desactivada hasta que definas una clave.

| Variable | Valor por defecto | Propósito |
|---|---|---|
| `M3M0S_EXTRACT_API_KEY` | ninguna (desactivada) | Activa la extracción de hechos con LLM. |
| `M3M0S_EXTRACT_BASE_URL` | `https://openrouter.ai/api/v1` | Endpoint compatible con OpenAI para la extracción. |
| `M3M0S_EXTRACT_MODEL` | `deepseek/deepseek-v4-flash` | Modelo de extracción. |
| `M3M0S_CHAT_API_KEY` | la clave de extracción | Activa el chat de la WebUI. |
| `M3M0S_CHAT_BASE_URL`, `M3M0S_CHAT_MODEL` | OpenRouter, `deepseek/deepseek-v4-flash` | Proveedor y modelo del chat. |

## Tareas en segundo plano

| Variable | Valor por defecto | Propósito |
|---|---|---|
| `M3M0S_RETRO_EXTRACT_INTERVAL` | `2h` | Intervalo de la tarea de extracción retroactiva. |
| `M3M0S_FORGET_SWEEP_INTERVAL` | `12h` | Intervalo del barrido de olvido. |
| `M3M0S_RETENTION_MODEL` | `legacy` | Puntuación que usa el barrido: `legacy` (una sola vida media de 30 días) o `per-kind` (vida media según la naturaleza del hecho, opcional). |
| `M3M0S_REEMBED_BATCH`, `M3M0S_REEMBED_RPS` | `64`, `5` | Tamaño de lote y tasa de solicitudes de la tarea de reembedding. |
| `M3M0S_JOBS_TICK` | `30s` | Pulso del planificador. |
| `M3M0S_INSIGHTS_CPU_PERCENT` | `60` | Ciclo de trabajo de CPU del análisis de salud de la memoria. |
| `M3M0S_INSIGHTS_MAX_MINUTES` | `30` | Límite de tiempo real de ese análisis. |

## Filtro de ingesta

La captura automática descarta o etiqueta el texto repetitivo de las herramientas antes de almacenarlo.

| Variable | Valor por defecto | Propósito |
|---|---|---|
| `M3M0S_FILTER_MODE` | `default` | Modo global: `default`, `off`, `tag` o `drop`. |
| `M3M0S_FILTER_MODES` | ninguna | Modos por regla que prevalecen sobre el global, por ejemplo `diff-trivia-fact=drop`. |
| `M3M0S_FILTER_RULES` | ninguna | Archivo JSON de reglas que reemplazan, añaden o desactivan las integradas. |

## Clientes

Estas variables las leen la CLI, el puente MCP, el watcher y los hooks.

| Variable | Valor por defecto | Propósito |
|---|---|---|
| `M3M0S_URL` | `http://localhost:19234` | URL del servidor. |
| `M3M0S_API_KEY` | ninguna | Clave de API. |
| `M3M0S_HOOK_DOMAIN` | derivado del directorio de trabajo | Dominio bajo el que se archivan las memorias capturadas. |

Las variables de ajuste de la recuperación están en la página [Recuperación y ajuste](/es/guide/recall/).

## Proveedor de embeddings en tiempo de ejecución

El modelo de embeddings, el endpoint, las dimensiones y la clave también se pueden cambiar sin reiniciar, desde la WebUI (página Embedding) o desde la CLI:

```sh
m3m0s embedding show
m3m0s embedding estimate --model text-embedding-3-large --dimensions 1024
echo "$NEW_KEY" | m3m0s embedding set --model text-embedding-3-large --api-key-stdin
m3m0s embedding reembed start --watch
```

La clave nunca se acepta como argumento ni se imprime. Si un cambio deja vectores almacenados con otro modelo o tamaño, `set` muestra cuántos son y un costo aproximado, y te pide confirmación.
