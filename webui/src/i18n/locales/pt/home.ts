import type { Messages } from "../../types";

export const home: Messages<typeof import("../en/home").home> = {
  "home.title": "Boas-vindas ao M3m0s",
  "home.subtitle": "Memória de longo prazo para você e seus agentes de IA.",
  "home.count.one": "Você tem {count} memória salva.",
  "home.count.other": "Você tem {count} memórias salvas.",
  "home.docsNote":
    "O M3m0s também inclui {count} páginas de documentação embutida. Elas ficam ocultas nas suas listas.",
  "home.checklist.title": "Primeiros passos",
  "home.checklist.progress": "{done} de {total} concluídos",
  "home.checklist.allDone":
    "Tudo pronto. Seus agentes agora podem lembrar das coisas entre uma sessão e outra.",
  "home.step.account.title": "Crie a sua conta",
  "home.step.account.body": "Concluído: você está conectado.",
  "home.step.memory.title": "Salve a sua primeira memória",
  "home.step.memory.body": "Anote uma coisa que vale lembrar, como uma decisão ou uma preferência.",
  "home.step.memory.action": "Adicionar uma memória",
  "home.step.agent.title": "Conecte o seu agente de IA",
  "home.step.agent.body": "Dê ao Claude Code ou a outra ferramenta o endereço deste servidor e uma chave.",
  "home.step.agent.action": "Conectar um agente",
  "home.step.search.title": "Ative a busca por significado",
  "home.step.search.body": "Opcional. Sem ela, a busca só encontra as palavras exatas que você digita.",
  "home.step.search.action": "Configurar embeddings",
  "home.done": "Concluído",
  "home.quick.title": "O que você quer fazer?",
  "home.quick.search": "Encontrar algo que você salvou",
  "home.quick.add": "Salvar uma nova memória",
  "home.quick.connect": "Conectar uma ferramenta de IA",
  "home.empty.title": "Sua memória está vazia",
  "home.empty.body":
    "Uma memória é uma nota curta, como \"Escolhemos o PostgreSQL para o banco de dados\". Salve algumas e seus agentes começarão a usá-las.",
  "home.empty.example": "Exemplo: \"Prefiro respostas curtas, com o código primeiro.\"",
};
