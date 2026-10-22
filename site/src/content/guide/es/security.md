---
title: Seguridad
description: Qué almacena m3m0s, cómo lo protege y cómo ejecutarlo de forma segura.
section: Reference
order: 12
---

m3m0s almacena todo lo que le envíes, incluidas las transcripciones de sesiones, y lo configuras con claves de API de proveedores. Trata el servidor y sus respaldos como cualquier otro sistema que guarda ese tipo de texto.

## Qué protege tus datos

- **Las claves de API** protegen la API HTTP, el puente MCP y la CLI. Las cuentas de la WebUI tienen nombre de usuario y contraseña, y el restablecimiento se hace con un enlace de un solo uso válido por 30 minutos.
- **La redacción de secretos** se ejecuta en la ingesta y de nuevo antes de enviar texto a un proveedor de extracción. Es de mejor esfuerzo: no puede detectar todos los formatos.
- **`m3m0s maintenance redact-scan`** reporta los secretos que se almacenaron antes de que existiera una regla de redacción. `--apply` vuelve a redactar los blobs de origen almacenados. Por defecto es una simulación.
- **El modo MCP de solo lectura** evita que los agentes reescriban la memoria. Es una comodidad, no un límite de seguridad: cualquiera que tenga la clave de API puede llamar a la API HTTP.
- **Los proveedores ven lo que les envías.** El texto va a los endpoints de embeddings, extracción y chat que configures, y por defecto no se configura ningún otro destino.

## Ejecútalo de forma segura

1. Coloca el servidor detrás de TLS y de un perímetro de red de confianza antes de exponerlo. Una red privada o una VPN es mejor que internet abierto.
2. Mantén `.env` y `~/.m3m0s/hook.env` fuera del control de versiones, este último con permisos `600`.
3. Trata los respaldos y el volumen de datos como información sensible.
4. Usa un proveedor de embeddings y de extracción en el que confíes para el texto que ven tus agentes, o apunta `M3M0S_OPENAI_BASE_URL` a un endpoint local.

## Cómo reportar una vulnerabilidad

No abras un issue público. Usa el reporte privado de vulnerabilidades de GitHub: abre la pestaña **Security** del repositorio y elige **Report a vulnerability**. Incluye la versión afectada, cómo ejecutas m3m0s y los pasos para reproducirlo usando únicamente datos sintéticos. Consulta [`SECURITY.md`](https://github.com/rafaelvieiras/m3m0s/blob/main/SECURITY.md) para conocer el alcance y las versiones con soporte.

Nunca publiques claves de API, transcripciones, contenido real de la memoria, archivos de base de datos ni nombres de host internos en issues públicos o en logs. Si se filtró un secreto, rótalo de inmediato.
