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
  "chat.empty":
    "Pregunta sobre tus memorias o pídeme que guarde algo nuevo. Respondo con lo que M3m0s recuerda. Escribe {slash} para ver los comandos. Si digo que el chat no está activado, el servidor necesita una clave de proveedor de IA para el chat.",
  "chat.typing": "escribiendo…",
  "chat.placeholder": "Pregunta o pide guardar algo… (/ para comandos)",
  "chat.send": "Enviar mensaje",
};
