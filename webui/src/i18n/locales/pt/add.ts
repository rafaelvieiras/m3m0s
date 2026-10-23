import type { Messages } from "../../types";

export const add: Messages<typeof import("../en/add").add> = {
  "add.added": "Memória adicionada",
  "add.title": "Adicionar uma memória",
  "add.subtitle":
    "Escreva algo que vale a pena lembrar. Se a extração de fatos estiver ligada, textos longos são divididos em fatos curtos e separados.",
  "add.content": "O que deve ser lembrado?",
  "add.contentRequired": "o conteúdo é obrigatório",
  "add.contentPlaceholder": "Escreva como contaria a um colega...",
  "add.domainHelp": "Formato área:nome, por exemplo writing:meu-livro. Se ficar vazio, vai para {default}.",
  "add.sourcePlaceholder": "manual (padrão)",
  "add.agent": "Agente (opcional)",
  "add.tags": "Etiquetas (separadas por vírgula)",
  "add.metadata": "Dados extras (JSON, opcional)",
  "add.metadataHelp": "Os valores devem ser texto simples.",
  "add.metadataJson": "Os dados extras devem ser um JSON válido",
  "add.metadataFlat":
    "Os dados extras devem ser um objeto JSON simples com valores de texto (sem aninhamento)",
  "add.submit": "Adicionar memória",
  "add.examples": "Experimente um exemplo:",
  "add.example1": "Usamos PostgreSQL no banco de dados porque ele lida bem com JSON.",
  "add.example2": "Prefiro respostas curtas, com o código primeiro.",
  "add.example3": "Para fazer deploy: rode make release e depois envie a tag.",
  "add.optional": "opcional",
  "add.advancedOptions": "Opções avançadas",
};
