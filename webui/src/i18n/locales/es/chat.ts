import type { Messages } from "../../types";

export const chat: Messages<typeof import("../en/chat").chat> = {
  "chat.cmd.clean": "Borra el historial de esta conversación",
  "chat.cleared": "Conversación borrada",
  "chat.unknownCommand": "Comando desconocido: /{name}",
  "chat.failed": "Falló el chat: {message}",
  "chat.closeMemoryChat": "Cerrar el chat de la memoria",
  "chat.openMemoryChat": "Abrir el chat de la memoria",
  "chat.region": "Chat de la memoria",
  "chat.title": "Asistente de la memoria",
  "chat.collapse": "Contraer panel",
  "chat.expand": "Expandir panel",
  "chat.close": "Cerrar chat",
  "chat.empty": "Pregunta algo sobre tus memorias o pídeme que guarde información nueva. Escribe {slash} para ver los comandos disponibles.",
  "chat.typing": "escribiendo…",
  "chat.placeholder": "Pregunta o pide guardar algo… (/ para comandos)",
  "chat.send": "Enviar mensaje",
};
