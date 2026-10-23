import type { Messages } from "../../types";

export const add: Messages<typeof import("../en/add").add> = {
  "add.added": "Memoria agregada",
  "add.title": "Añadir una memoria",
  "add.subtitle":
    "Escribe algo que valga la pena recordar. Si la extracción de hechos está activada, los textos largos se dividen en hechos cortos y separados.",
  "add.content": "¿Qué hay que recordar?",
  "add.contentRequired": "el contenido es obligatorio",
  "add.contentPlaceholder": "Escríbelo como se lo contarías a un compañero...",
  "add.domainHelp":
    "Formato área:nombre, por ejemplo writing:mi-libro. Si se deja vacío, se archiva en {default}.",
  "add.sourcePlaceholder": "manual (predeterminado)",
  "add.agent": "Agente (opcional)",
  "add.tags": "Etiquetas (separadas por comas)",
  "add.metadata": "Datos extra (JSON, opcional)",
  "add.metadataHelp": "Los valores deben ser texto simple.",
  "add.metadataJson": "Los datos extra deben ser un JSON válido",
  "add.metadataFlat":
    "Los datos extra deben ser un objeto JSON simple con valores de texto (sin anidamiento)",
  "add.submit": "Agregar memoria",
  "add.examples": "Prueba un ejemplo:",
  "add.example1": "Usamos PostgreSQL como base de datos porque maneja bien JSON.",
  "add.example2": "Prefiero respuestas cortas, con el código primero.",
  "add.example3": "Para desplegar: ejecuta make release y luego sube la etiqueta.",
  "add.optional": "opcional",
  "add.advancedOptions": "Opciones avanzadas",
};
