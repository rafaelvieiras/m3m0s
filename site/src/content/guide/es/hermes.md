---
title: Hermes
description: Un proveedor de memoria para agentes Hermes que recupera antes de cada turno, captura después y agrega una herramienta de búsqueda de solo lectura.
section: Connect your tools
order: 9
---

La integración con Hermes es un **proveedor de memoria**. Reutiliza el binario `m3m0s`, por lo que los filtros de relevancia y la redacción de secretos se comportan exactamente igual que en las demás herramientas.

- **Recuperación** (`prefetch`): ejecuta `m3m0s hook claude-code --inject` con el mensaje del usuario. Hermes envuelve el resultado en su propio delimitador `<memory-context>`.
- **Captura** (`sync_turn` como `Stop`, `on_session_end` como `SessionEnd`): agrega cada turno a una transcripción por sesión y la entrega a `m3m0s hook claude-code`. Las memorias llevan `source: "hermes"` y se archivan bajo `M3M0S_HOOK_DOMAIN` (por defecto `personal:hermes`).
- **Herramienta** `m3m0s_search` (solo lectura): `query`, `limit`, `domain`, o `recent` para listar las memorias más recientes de un dominio.

Los turnos de cron y de webhook se omiten. Los fallos son silenciosos y nunca bloquean al agente.

## Instalación

Hermes admite un único proveedor de memoria externo.

### Con la CLI de Hermes

El binario debe estar en el `PATH`, o en `<plugin dir>/bin/m3m0s`:

```sh
hermes plugins install rafaelvieiras/m3m0s/integrations/hermes/m3m0s-memory
hermes memory setup   # pick m3m0s-memory
```

### Instalación manual

Los pasos siguientes suponen que el agente se ejecuta en un contenedor con un volumen de datos montado en `<data>`.

1. Compila un binario estático para la plataforma del agente:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o m3m0s ./cmd/m3m0s
```

2. Copia el plugin y el binario al volumen de datos, con el usuario del agente como propietario:

```sh
cp -r integrations/hermes/m3m0s-memory <data>/plugins/m3m0s-memory
install -m 755 m3m0s <data>/plugins/m3m0s-memory/bin/m3m0s
```

3. Agrega las credenciales a `<data>/.env`:

```sh
M3M0S_URL=https://memory.example.com
M3M0S_API_KEY=<your API key>
M3M0S_HOOK_DOMAIN=personal:hermes   # optional
```

4. Selecciona el proveedor en `<data>/config.yaml` y reinicia el agente:

```yaml
memory:
  provider: m3m0s-memory
```

`hermes memory status` debería mostrarlo como activo. El estado (checkpoints, cola y transcripciones) vive en `<data>/m3m0s/`.
