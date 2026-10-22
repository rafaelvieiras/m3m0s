---
title: CLI y credenciales
description: Instala el binario m3m0s en la máquina donde corren tus agentes, guarda las credenciales una sola vez y comprueba que la captura funciona.
section: Connect your tools
order: 5
---

Todas las integraciones reutilizan el binario `m3m0s`, así que instálalo en cada máquina donde ejecutes un agente. Es el mismo binario que el servidor.

## Instala el binario

Descarga el archivo para tu arquitectura desde la [página de releases](https://github.com/rafaelvieiras/m3m0s/releases) (Linux `amd64`, `arm64`, `armv7`) y coloca `m3m0s` en tu `PATH`. O compílalo con Go:

```sh
go install github.com/rafaelvieiras/m3m0s/cmd/m3m0s@latest
```

Se instala en `~/go/bin/m3m0s`. Asegúrate de que ese directorio esté en el `PATH` de la herramienta que lanza los hooks.

## Guarda las credenciales

Los hooks y plugins leen la URL del servidor y la clave de API desde el entorno, y recurren a `~/.m3m0s/hook.env` como alternativa. Crea ese archivo una sola vez y todas las integraciones lo usarán:

```sh
mkdir -p ~/.m3m0s
cat > ~/.m3m0s/hook.env <<'ENV'
M3M0S_URL=https://memory.example.com
M3M0S_API_KEY=<your API key>
ENV
chmod 600 ~/.m3m0s/hook.env
```

Una línea opcional `M3M0S_HOOK_DOMAIN` cambia el dominio bajo el que se archivan las memorias capturadas. Sin ella, el dominio se deriva del directorio de trabajo (`code:<folder name>`).

## Comprueba que funciona

```sh
m3m0s doctor
```

`doctor` verifica las credenciales, la conexión con el servidor, el registro del hook de Claude Code y la cola de capturas que no pudieron enviarse. Termina con código de salida 1 cuando algo falla e indica cómo corregirlo. `m3m0s doctor --replay` reenvía la cola pendiente.

Los hooks siempre terminan con código 0, porque nunca deben romper el agente anfitrión. Su log es `~/.m3m0s/hooks/claude-code.log`.

## Comandos de uso diario

```sh
m3m0s add "Deploys go through the staging branch first" --domain code:myproject
m3m0s search "how do we deploy?" --domain code:myproject
m3m0s search "retry policy" --kind rule
m3m0s recent --limit 10
m3m0s get <id>
m3m0s update <id> --kind gotcha
m3m0s stats
```

La lista completa de operaciones, para MCP, CLI y HTTP, está en `docs/api-contract.md` en el repositorio.

## Vigila la documentación de un proyecto

```sh
m3m0s watch ~/projects/myproject
```

El watcher indexa `AGENTS.md`, `CLAUDE.md`, `MEMORY.md` y `docs/**/*.md`, y los reindexa cuando cambian.
