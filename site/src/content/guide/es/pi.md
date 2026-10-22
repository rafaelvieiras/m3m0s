---
title: pi
description: Una extensión para el agente de programación pi que recupera memoria antes de cada ejecución y captura las sesiones después.
section: Connect your tools
order: 7
---

La extensión de pi le da a pi el mismo ciclo de memoria que los hooks de Claude Code, llamando al binario `m3m0s`.

- **Recuperación** (`before_agent_start`): ejecuta `m3m0s hook claude-code --inject` con el prompt y agrega las memorias relevantes como un mensaje personalizado oculto. El bloque está delimitado con `<memory-context>` y lleva una nota que indica que es memoria recuperada y no una nueva entrada del usuario.
- **Captura** (`agent_end`, `session_shutdown`): ejecuta `m3m0s hook claude-code` sobre el archivo de sesión de pi. Las memorias se almacenan con `source: "pi"`.

No se inyecta nada durante la compactación. La recuperación ocurre en cada prompt.

## Instalación

1. Instala el binario y guarda las credenciales: [CLI y credenciales](/es/guide/cli/). La extensión busca `m3m0s` primero en `~/go/bin/m3m0s` y después en el `PATH`.
2. Instala el paquete desde npm:

```sh
pi install npm:m3m0s-pi
```

3. Reinicia pi. Define `M3M0S_BIN` si el binario está en otra ubicación.

### Instalación manual

En su lugar, copia la extensión en la carpeta de extensiones de pi:

```sh
mkdir -p ~/.pi/agent/extensions
curl -fsSL https://raw.githubusercontent.com/rafaelvieiras/m3m0s/main/integrations/pi/m3m0s-memory.ts \
  -o ~/.pi/agent/extensions/m3m0s-memory.ts
```


Luego reinicia pi.

## Opcional: MCP de solo lectura

Registra el servidor en `~/.pi/agent/mcp.json`:

```json
{
  "m3m0s": {
    "command": "sh",
    "args": ["-c", "set -a; . \"$HOME/.m3m0s/hook.env\"; exec m3m0s mcp --read-only"]
  }
}
```

## Solución de problemas

Los fallos son silenciosos y nunca bloquean a pi. Lee `~/.m3m0s/hooks/claude-code.log` y ejecuta `m3m0s doctor`. Si aparece una memoria de otro proyecto, consulta [Recuperación y ajuste](/es/guide/recall/).
