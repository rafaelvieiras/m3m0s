import type { Messages } from "../../types";

export const preview: Messages<typeof import("../en/preview").preview> = {
  "preview.mode.hook": "Automático (Claude Code)",
  "preview.mode.mcp": "Bajo demanda (MCP)",
  "preview.mode.manual": "Copiar y pegar",
  "preview.copy": "Copiar",
  "preview.copied": "Copiado",
  "preview.title": "Lo que verá el agente",
  "preview.description":
    "Las memorias que tu agente de IA recibe para un tema, exactamente como se le entregarían hoy.",
  "preview.domain": "Tema",
  "preview.domainPlaceholder": "code:mi-app (vacío = todos los temas)",
  "preview.apply": "Aplicar",
  "preview.loadFailed": "No se pudo cargar: {message}",
  "preview.allDomains": "todos los temas",
  "preview.fetched": "{count} memorias encontradas",
  "preview.used": "{count} usadas en el resultado",
  "preview.empty.title": "El agente no recibiría nada",
  "preview.empty.description": "Aún no hay memorias para este tema y modo. Añade algunas o elige otro tema.",
  "preview.extraction.on":
    "La extracción de hechos está activada: se crearon {count} hecho(s) a partir de tu texto.",
  "preview.extraction.off":
    "La extracción de hechos está desactivada, así que el texto se guardó tal como se escribió.",
  "preview.extraction.original": "Texto original",
  "preview.extraction.fact": "Hecho {n}",
  "preview.extraction.created": "Memoria creada",
  "preview.extraction.deduped": "deduplicada (fusionada con la existente)",
  "preview.extraction.new": "nueva",
  "preview.extraction.addAnother": "Agregar otra memoria",
};
