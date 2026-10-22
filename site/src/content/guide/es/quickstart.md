---
title: Inicio rápido
description: Ejecuta el servidor con Docker Compose, crea tu cuenta y guarda tu primera memoria en unos cinco minutos.
section: Start
order: 2
---

Necesitas Docker con Compose v2.24 o posterior y una clave de API compatible con OpenAI para los embeddings. Sin clave, el servidor funciona igualmente, en modo solo de palabras clave (búsqueda de texto completo y deduplicación por coincidencia exacta).

## 1. Inicia el servidor

```sh
git clone https://github.com/rafaelvieiras/m3m0s.git
cd m3m0s
cp .env.example .env        # set OPENAI_API_KEY
docker compose up -d
curl http://localhost:19234/health
docker compose logs         # the first start prints a generated API key
```

> Guarda la clave de API que aparece en el log. Se muestra una sola vez y no se vuelve a imprimir.

## 2. Crea tu cuenta

Abre `http://localhost:19234/`. Una instancia nueva ejecuta una configuración inicial: crea la primera cuenta con un nombre de usuario y una contraseña, sin necesidad de clave. Hazlo de inmediato si la instancia es accesible desde internet, porque quien la abra primero se queda con ella. A partir de ahí, inicias sesión con esa cuenta.

Las cuentas son para las personas que usan la WebUI. Los hooks, MCP y la CLI siguen usando claves de API.

## 3. Obtén la CLI

La CLI es el mismo binario que el servidor. Descarga un archivo para Linux (`amd64`, `arm64` o `armv7`) desde la [página de releases](https://github.com/rafaelvieiras/m3m0s/releases), o compílalo:

```sh
go install github.com/rafaelvieiras/m3m0s/cmd/m3m0s@latest
```

Apúntala al servidor:

```sh
export M3M0S_URL=http://localhost:19234
export M3M0S_API_KEY=<the key from the first start>

m3m0s add "We use SQLite with versioned migrations" --domain code:myproject
m3m0s search "which database do we use?" --domain code:myproject
```

## 4. Conecta una herramienta

Elige la herramienta que usas y sigue su página: [Claude Code](/es/guide/claude-code/), [pi](/es/guide/pi/), [opencode](/es/guide/opencode/), [Hermes](/es/guide/hermes/) o [un cliente MCP](/es/guide/mcp/). Todas leen los mismos dos ajustes, `M3M0S_URL` y `M3M0S_API_KEY`.

## ¿Olvidaste la contraseña?

`m3m0s user reset <username>` imprime un enlace de un solo uso, válido por 30 minutos, que abre una página para elegir una nueva contraseña. Ejecútalo con `M3M0S_URL` y `M3M0S_API_KEY` definidas, o dentro del contenedor. `m3m0s user add`, `list` y `delete` administran cuentas adicionales, y todas comparten la misma memoria.
