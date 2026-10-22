---
title: opencode
description: Un plugin para opencode que agrega las memorias relevantes a cada turno y captura las sesiones cuando quedan inactivas.
section: Connect your tools
order: 8
---

El plugin de opencode le da a [opencode](https://opencode.ai) el mismo ciclo de memoria que los hooks de Claude Code.

- **Recuperación:** cada mensaje del usuario se busca en m3m0s y los resultados relevantes se agregan al prompt de sistema de ese turno.
- **Captura:** en `session.idle` (como `Stop`) y al cerrar (`SessionEnd`), el texto de la sesión se escribe en `~/.m3m0s/hooks/opencode/<session>.jsonl` y se entrega a `m3m0s hook claude-code`. Las memorias llevan `source: "opencode"`.

## Instalación

1. Instala el binario y guarda las credenciales: [CLI y credenciales](/es/guide/cli/). Define `M3M0S_BIN` si el binario no está en `~/go/bin/m3m0s` ni en el `PATH`.
2. Agrega el paquete de npm al arreglo `plugin` de `opencode.json`. opencode lo instala al iniciar:

```json
{
  "plugin": ["m3m0s-opencode"]
}
```

### Instalación manual

O coloca el archivo en la carpeta de plugins de opencode:

```sh
mkdir -p ~/.config/opencode/plugins
curl -fsSL https://raw.githubusercontent.com/rafaelvieiras/m3m0s/main/integrations/opencode/m3m0s-memory.js \
  -o ~/.config/opencode/plugins/m3m0s-memory.js
```

Luego agrégalo a `opencode.json`:

```json
{
  "plugin": ["./plugins/m3m0s-memory.js"]
}
```

## Opcional: MCP de solo lectura

Para que el agente también pueda buscar de forma explícita:

```json
{
  "mcp": {
    "m3m0s": {
      "type": "local",
      "enabled": true,
      "command": ["sh", "-c", "set -a; . \"$HOME/.m3m0s/hook.env\"; exec m3m0s mcp --read-only"]
    }
  }
}
```

## Solución de problemas

Los fallos son silenciosos y nunca bloquean a opencode. El log es `~/.m3m0s/hooks/claude-code.log`. Ejecuta `m3m0s doctor` para comprobar las credenciales y la conexión.
