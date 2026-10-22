---
title: Claude Code
description: Captura sesiones y recupera memoria en cada prompt en Claude Code, con hooks y un servidor MCP opcional de solo lectura.
section: Connect your tools
order: 6
---

Para Claude Code, m3m0s es un conjunto de hooks que ejecutan todos `m3m0s hook claude-code`. Instala primero el binario y guarda las credenciales, como se describe en [CLI y credenciales](/es/guide/cli/).

## Qué hacen los hooks

| Evento | Función |
|---|---|
| `SessionStart` | Inyecta un resumen de las memorias recientes. |
| `UserPromptSubmit` | Dos entradas. Una síncrona con `--inject` busca en la memoria según el prompt e inyecta los resultados relevantes. Otra asíncrona captura. |
| `Stop`, `PreCompact`, `SessionEnd` | Capturan la parte nueva de la transcripción (`PreCompact` y `SessionEnd` pueden ser asíncronos). |
| `PostToolUse` | Red de seguridad opcional. No hace nada hasta que la transcripción haya crecido. |

Los hooks asíncronos no pueden inyectar contexto, por eso la entrada con `--inject` debe ser síncrona.

## Instala el plugin

El plugin incluye todos los hooks de abajo y el servidor MCP de solo lectura. Instala primero el binario y `~/.m3m0s/hook.env`; luego, en Claude Code:

```
/plugin marketplace add rafaelvieiras/m3m0s
/plugin install m3m0s-memory@m3m0s
```

Desde la shell, el equivalente es `claude plugin marketplace add rafaelvieiras/m3m0s` seguido de `claude plugin install m3m0s-memory@m3m0s`. Si ya configuraste estos hooks en `settings.json`, quítalos primero para que no se ejecute todo por duplicado. El plugin también cubre el servidor MCP, así que omite la sección opcional de más abajo.

## O configura los hooks a mano

Si prefieres no usar el plugin, agrega esto a `~/.claude/settings.json`:

```json
{
  "hooks": {
    "SessionStart": [
      { "hooks": [{ "type": "command", "timeout": 15,
        "command": "m3m0s hook claude-code 2>>~/.m3m0s/hooks/claude-code.log || true" }] }
    ],
    "UserPromptSubmit": [
      { "hooks": [{ "type": "command", "timeout": 60, "async": true,
        "command": "m3m0s hook claude-code 2>>~/.m3m0s/hooks/claude-code.log || true" }] },
      { "hooks": [{ "type": "command", "timeout": 10,
        "command": "m3m0s hook claude-code --inject 2>>~/.m3m0s/hooks/claude-code.log || true" }] }
    ],
    "Stop": [
      { "hooks": [{ "type": "command", "timeout": 60,
        "command": "m3m0s hook claude-code 2>>~/.m3m0s/hooks/claude-code.log || true" }] }
    ],
    "PreCompact": [
      { "hooks": [{ "type": "command", "timeout": 60, "async": true,
        "command": "m3m0s hook claude-code 2>>~/.m3m0s/hooks/claude-code.log || true" }] }
    ],
    "SessionEnd": [
      { "hooks": [{ "type": "command", "timeout": 60, "async": true,
        "command": "m3m0s hook claude-code 2>>~/.m3m0s/hooks/claude-code.log || true" }] }
    ]
  }
}
```

Crea el directorio de logs una sola vez con `mkdir -p ~/.m3m0s/hooks` y luego ejecuta `m3m0s doctor` para confirmar que los eventos están registrados.

## Opcional: servidor MCP de solo lectura (solo con instalación manual)

Con la captura y la recuperación automáticas, el agente solo necesita buscar de forma explícita de vez en cuando. Registra el servidor MCP en modo de solo lectura, para que no pueda agregar ni reescribir memorias:

```sh
claude mcp add m3m0s --scope user \
  -e M3M0S_URL=https://memory.example.com \
  -e M3M0S_API_KEY=<your API key> \
  -- m3m0s mcp --read-only
```

Consulta [Clientes MCP](/es/guide/mcp/) para ver las herramientas que expone.

## Verifica

Abre una sesión nueva en un proyecto del que tengas memorias y pregunta por algo que decidiste antes. Una memoria que coincida se inyecta antes de que el modelo responda. Cada inyección se registra en `~/.m3m0s/hooks/claude-code.log`.
