---
title: Despliegue
description: Ejecuta m3m0s en un servidor con Docker Compose o con el binario directo, detrás de TLS, con respaldos y actualizaciones.
section: Deploy
order: 3
---

m3m0s almacena todo lo que le envíes, incluidas las transcripciones de conversaciones. Colócalo detrás de TLS y de un perímetro de red de confianza antes de exponerlo a internet, y lee primero la página de [Seguridad](/es/guide/security/).

## Docker Compose

El repositorio incluye un `compose.yaml` que construye la imagen a partir del `Dockerfile` de la raíz (una compilación multietapa: WebUI con Vite y luego un binario estático de Go con la WebUI embebida). La imagen es Go puro, sin cgo, así que también se compila y se ejecuta en placas ARM pequeñas.

```sh
cp .env.example .env
docker compose up -d --build
```

- El puerto publicado en el host es `M3M0S_HOST_PORT` (por defecto `19234`).
- Los datos viven en el volumen con nombre `m3m0s-data`, montado en `/data`. Contiene la base de datos SQLite y el archivo de la clave de API. Un volumen con nombre (no un bind mount) conserva el propietario correcto para el usuario sin privilegios dentro de la imagen.
- La imagen incluye un healthcheck integrado en `GET /health`.
- Los secretos provienen de `.env`. Mantenlo fuera del control de versiones.

Las imágenes también se publican en `ghcr.io/rafaelvieiras/m3m0s`, etiquetadas por commit (`sha-<commit>`). Si despliegas con una herramienta GitOps, apúntala a esa imagen en lugar de compilar en el host.

## Detrás de un proxy inverso

Termina el TLS delante del contenedor. Por ejemplo, con Caddy:

```caddyfile
memory.example.com {
    reverse_proxy localhost:19234
}
```

No se necesita nada más: la WebUI y la API comparten el mismo puerto.

## Sin Docker

Descarga el archivo para tu arquitectura desde la [página de releases](https://github.com/rafaelvieiras/m3m0s/releases) y ejecútalo:

```sh
M3M0S_DATA_DIR=/var/lib/m3m0s OPENAI_API_KEY=sk-... m3m0s serve
```

Una unidad mínima de systemd:

```ini
[Unit]
Description=m3m0s memory server
After=network-online.target

[Service]
User=m3m0s
EnvironmentFile=/etc/m3m0s.env
Environment=M3M0S_DATA_DIR=/var/lib/m3m0s
ExecStart=/usr/local/bin/m3m0s serve
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

Para compilar desde el código fuente con la WebUI embebida, ejecuta primero `npm ci && npm run build` en `webui/` y luego `go build ./cmd/m3m0s`.

## Equipos pequeños

En una placa con poca RAM, define un límite blando de memoria para que el runtime de Go recolecte antes, por ejemplo `GOMEMLIMIT=600MiB`. Mantenlo por encima del tamaño en uso, que es de unos 250 MB para 75 000 memorias. `GOGC` vale 50 por defecto, lo que ya cambia un poco de CPU por un pico de memoria menor.

## Respaldos

Todo el estado está en el directorio de datos. Para obtener una copia consistente, detén el contenedor, archiva el volumen y vuelve a iniciarlo:

```sh
docker compose stop m3m0s
docker run --rm -v m3m0s-data:/data -v "$PWD":/backup alpine \
  tar czf /backup/m3m0s-data.tgz -C /data .
docker compose start m3m0s
```

La memoria puede contener secretos que se guardaron antes de que existiera una regla de redacción. Trata los respaldos como información sensible. `m3m0s maintenance redact-scan` reporta esos casos, y `--apply` vuelve a redactar los blobs de origen almacenados (por defecto es una simulación).

## Actualización

```sh
git pull
docker compose up -d --build
```

El esquema de la base de datos se migra al iniciar mediante migraciones versionadas. Haz un respaldo antes si vas a saltar varias versiones.
