---
title: Recuperación y ajuste
description: Cómo decide la recuperación por prompt qué inyectar, qué parámetros existen y qué revisar cuando aparece una memoria (o no aparece).
section: Reference
order: 11
---

La recuperación se ejecuta en cada prompt no trivial. Está pensada para ser discreta: cuando nada es claramente relevante, no se inyecta nada.

## Qué ocurre en cada prompt

1. Los prompts de menos de 20 caracteres y los comandos con barra se omiten sin hacer ninguna llamada.
2. El prompt se convierte en embedding y se busca en modo **vectorial**. Solo la búsqueda vectorial produce una puntuación de coseno a la que se le puede aplicar un umbral.
3. Los resultados deben alcanzar una puntuación de coseno de **0.60**. Se conservan como máximo **3** entradas y unos **500** tokens estimados.
4. Un filtro de tema descarta los resultados que no comparten ningún término con el prompt. Los nombres mal escritos se comparan de forma aproximada, y los términos de la consulta se corrigen del lado del servidor.
5. Un resultado cuyo dominio es **otro proyecto** (`code:<other-folder>`) debe superar el umbral más 0.10, para que una nota vagamente relacionada de un repositorio ajeno no se cuele. Los dominios fijados que no son de código, como el que usa el proveedor de Hermes, mantienen el umbral normal.
6. Las memorias ya inyectadas en la sesión se recuerdan y no se repiten.

Las entradas de respaldo sin procesar nunca se inyectan. La llamada tiene un tiempo límite de 4 segundos, normalmente muy por debajo de un segundo, y todo fallo es silencioso.

Los clientes que añaden el bloque por su cuenta (pi, opencode) lo delimitan como memoria recuperada, con una nota de que no es entrada del usuario, igual que hace Hermes.

## Ajuste

| Variable | Efecto |
|---|---|
| `M3M0S_PROMPT_INJECT=off` | Desactiva la recuperación por prompt. La captura sigue funcionando. |
| `M3M0S_PROMPT_MIN_SCORE` | Umbral de coseno, por defecto `0.60`. Súbelo para obtener menos resultados, pero más seguros. |
| `M3M0S_PROMPT_FOREIGN_BOOST=off` | Elimina el 0.10 adicional para las memorias de otros proyectos. |

Defínelas en el entorno de la herramienta que lanza el hook.

## ¿Está funcionando?

Cada inyección se registra en `~/.m3m0s/hooks/claude-code.log`, con la puntuación de cada resultado. En una muestra de producción, los prompts relevantes puntuaron entre 0.66 y 0.68 en el mejor resultado y los no relacionados alcanzaron un máximo de alrededor de 0.40, así que 0.60 los separa con poco margen. Si ves ruido, sube un poco el umbral; si las memorias útiles nunca aparecen, bájalo un poco y observa el log.

Un prompt como "adelante" no tiene un tema propio, así que no recuperará nada. La recuperación no reescribe las consultas a partir de la conversación.

## Dominios

Las memorias capturadas se archivan bajo un **dominio**, por defecto `code:<folder name>` para el directorio donde se inició la sesión. El dominio, no el proyecto, es el alcance real: es lo que usa la recuperación para distinguir tu propio proyecto de otro, y lo que pasas a `--domain` en la CLI. Define `M3M0S_HOOK_DOMAIN` para reemplazarlo en una herramienta.
