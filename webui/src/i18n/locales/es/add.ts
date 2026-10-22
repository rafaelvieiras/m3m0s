import type { Messages } from "../../types";

export const add: Messages<typeof import("../en/add").add> = {
  "add.added": "Memoria agregada",
  "add.title": "Agregar memoria",
  "add.subtitle":
    "POST /memories — si la extracción de hechos está habilitada en el servidor, el texto se convierte en N hechos atómicos (Fase 4/D12).",
  "add.newEntry": "Nueva entrada",
  "add.content": "Contenido",
  "add.contentRequired": "el contenido es obligatorio",
  "add.contentPlaceholder": "El texto/hecho a recordar...",
  "add.domainHelp": "categoría:instancia — p. ej.: writing:book-x",
  "add.sourcePlaceholder": "manual (predeterminado)",
  "add.agent": "agent (opcional)",
  "add.tags": "tags (separadas por coma)",
  "add.metadata": "metadata (JSON, opcional)",
  "add.metadataHelp": "Los valores deben ser strings (misma limitación del map[string]string de chromem-go).",
  "add.metadataJson": "metadata debe ser un JSON válido",
  "add.metadataFlat": "metadata debe ser un objeto JSON plano de string:string (sin anidamiento)",
  "add.submit": "Agregar memoria",
};
