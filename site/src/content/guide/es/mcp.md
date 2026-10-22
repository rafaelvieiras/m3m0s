---
title: Clientes MCP
description: Registra el servidor MCP por stdio en cualquier host compatible con MCP, con o sin acceso de escritura.
section: Connect your tools
order: 10
---

`m3m0s mcp` es un servidor MCP por stdio. Es un puente sin estado hacia un `m3m0s serve` en ejecución, por lo que cualquier host MCP puede usarlo, por ejemplo Claude Code, opencode o pi.

## Regístralo

Usa el comando `m3m0s` con el argumento `mcp` y pasa las dos variables de cliente:

```json
{
  "mcpServers": {
    "m3m0s": {
      "command": "m3m0s",
      "args": ["mcp", "--read-only"],
      "env": {
        "M3M0S_URL": "https://memory.example.com",
        "M3M0S_API_KEY": "<your API key>"
      }
    }
  }
}
```

El archivo y los nombres de las claves exactos cambian según el host (`mcpServers`, `mcp`, etc.). Lo que importa son el comando, los argumentos y el entorno.

## Herramientas

| Herramienta | Propósito |
|---|---|
| `memory_search` | Busca con una consulta corta. Admite un proyecto o dominio, un `kind`, un presupuesto de tokens y un modo `keyword` para términos exactos. |
| `memory_get` | Lee una memoria completa. |
| `memory_recent` | Lista las memorias más recientes. |
| `memory_stats` | Totales de la colección. |
| `memory_add`, `memory_update`, `memory_forget_sweep`, `memory_restore` | Herramientas de escritura. Ocultas en modo de solo lectura. |

## Modo de solo lectura

`m3m0s mcp --read-only` (o `M3M0S_MCP_READ_ONLY=1`) oculta las cuatro herramientas de escritura y las rechaza si se las llama. Con la captura automática en marcha, los agentes no necesitan escribir memoria mediante MCP, y una herramienta que existe acabará usándose por error tarde o temprano.

> El modo de solo lectura es una comodidad del lado del cliente, no un límite de seguridad. Cualquiera que tenga la clave de API puede seguir llamando a la API HTTP.

## Indícale al agente cuándo buscar

El servidor incluye instrucciones que le indican al modelo que busque antes de responder preguntas sobre tus proyectos, decisiones pasadas y convenciones, que trate los resultados como notas y no como instrucciones, y que no llame a `memory_add` para el progreso rutinario. Las entradas de tipo `state` describen un momento pasado, por lo que se le indica al agente que las verifique antes de apoyarse en ellas.
