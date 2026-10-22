import type { Messages } from "../../types";

export const add: Messages<typeof import("../en/add").add> = {
  "add.added": "Memória adicionada",
  "add.title": "Adicionar memória",
  "add.subtitle":
    "POST /memories — se a extração de fatos estiver habilitada no servidor, o texto vira N fatos atômicos (Fase 4/D12).",
  "add.newEntry": "Nova entrada",
  "add.content": "Conteúdo",
  "add.contentRequired": "o conteúdo é obrigatório",
  "add.contentPlaceholder": "O texto/fato a ser lembrado...",
  "add.domainHelp": "categoria:instância — ex.: writing:book-x",
  "add.sourcePlaceholder": "manual (padrão)",
  "add.agent": "agent (opcional)",
  "add.tags": "tags (separadas por vírgula)",
  "add.metadata": "metadata (JSON, opcional)",
  "add.metadataHelp": "Os valores devem ser strings (mesma limitação do map[string]string do chromem-go).",
  "add.metadataJson": "metadata deve ser um JSON válido",
  "add.metadataFlat": "metadata deve ser um objeto JSON plano de string:string (sem aninhamento)",
  "add.submit": "Adicionar memória",
};
