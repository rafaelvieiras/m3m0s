import type { Messages } from "../../types";

export const chat: Messages<typeof import("../en/chat").chat> = {
  "chat.cmd.clean": "Limpa o histórico desta conversa",
  "chat.cleared": "Conversa limpa",
  "chat.unknownCommand": "Comando desconhecido: /{name}",
  "chat.failed": "Falha no chat: {message}",
  "chat.closeMemoryChat": "Fechar o chat da memória",
  "chat.openMemoryChat": "Abrir o chat da memória",
  "chat.region": "Chat da memória",
  "chat.title": "Assistente da memória",
  "chat.collapse": "Recolher painel",
  "chat.expand": "Expandir painel",
  "chat.close": "Fechar chat",
  "chat.empty": "Pergunte algo sobre as suas memórias ou peça para eu guardar uma informação nova. Digite {slash} para ver os comandos disponíveis.",
  "chat.typing": "digitando…",
  "chat.placeholder": "Pergunte ou peça para guardar algo… (/ para comandos)",
  "chat.send": "Enviar mensagem",
};
