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
  "chat.empty":
    "Pergunte sobre suas memórias ou peça para eu salvar algo novo. Respondo com base no que o M3m0s lembra. Digite {slash} para ver os comandos. Se eu disser que o chat não está ativado, o servidor precisa de uma chave de provedor de IA para o chat.",
  "chat.typing": "digitando…",
  "chat.placeholder": "Pergunte ou peça para guardar algo… (/ para comandos)",
  "chat.send": "Enviar mensagem",
};
