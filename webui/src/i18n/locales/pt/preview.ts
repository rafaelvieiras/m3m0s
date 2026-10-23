import type { Messages } from "../../types";

export const preview: Messages<typeof import("../en/preview").preview> = {
  "preview.mode.hook": "Automático (Claude Code)",
  "preview.mode.mcp": "Sob demanda (MCP)",
  "preview.mode.manual": "Copiar e colar",
  "preview.copy": "Copiar",
  "preview.copied": "Copiado",
  "preview.title": "O que o agente vai ver",
  "preview.description":
    "As memórias que o seu agente de IA recebe para um tópico, exatamente como seriam entregues hoje.",
  "preview.domain": "Tópico",
  "preview.domainPlaceholder": "code:meu-app (vazio = todos os tópicos)",
  "preview.apply": "Aplicar",
  "preview.loadFailed": "Falha ao carregar: {message}",
  "preview.allDomains": "todos os tópicos",
  "preview.fetched": "{count} memórias encontradas",
  "preview.used": "{count} usadas no resultado",
  "preview.empty.title": "O agente não receberia nada",
  "preview.empty.description":
    "Ainda não há memórias para este tópico e modo. Adicione algumas ou escolha outro tópico.",
  "preview.extraction.on":
    "A extração de fatos está ligada: {count} fato(s) foram criados a partir do seu texto.",
  "preview.extraction.off": "A extração de fatos está desligada, então o texto foi salvo como escrito.",
  "preview.extraction.original": "Texto original",
  "preview.extraction.fact": "Fato {n}",
  "preview.extraction.created": "Memória criada",
  "preview.extraction.deduped": "deduplicada (mesclada com a existente)",
  "preview.extraction.new": "nova",
  "preview.extraction.addAnother": "Adicionar outra memória",
};
