import type { Messages } from "../../types";

export const help: Messages<typeof import("../en/help").help> = {
  "help.about": "Sobre esta página",
  "help.more": "O que é isto?",
  "help.close": "Fechar",
  "help.term": "Explicar: {term}",
  "help.term.memory.name": "Memória",
  "help.term.memory.body":
    "Uma nota curta que o M3m0s guarda para você e seus agentes de IA: uma decisão, uma regra, uma preferência ou qualquer coisa que valha lembrar.",
  "help.term.kind.name": "Tipo de memória",
  "help.term.kind.body":
    "O que a nota é: uma decisão, uma regra, uma preferência, uma armadilha, um passo a passo, uma situação atual ou outra coisa. Ajuda o M3m0s a decidir o que vale guardar.",
  "help.term.domain.name": "Tópico",
  "help.term.domain.body":
    "O assunto a que uma memória pertence, escrito como área:nome, por exemplo code:meu-app ou personal:saude. Mantém as partes da sua vida separadas.",
  "help.term.project.name": "Projeto",
  "help.term.project.body":
    "Uma etiqueta opcional que liga uma memória a um projeto. Memórias sem projeto são compartilhadas por todos eles.",
  "help.term.embedding.name": "Embeddings",
  "help.term.embedding.body":
    "Uma forma de comparar o significado dos textos. Com embeddings ligados, buscar \"carro\" também pode achar uma memória sobre \"veículo\".",
  "help.term.jobs.name": "Tarefas",
  "help.term.jobs.body":
    "Rotinas que o servidor executa em segundo plano, como limpar memórias antigas. Quase nunca é preciso mexer nelas.",
  "help.term.insights.name": "Insights",
  "help.term.insights.body":
    "Um relatório de saúde das suas memórias: quantas são, quão antigas e quais parecem duplicadas ou de pouco valor.",
  "help.term.preview.name": "O que o agente vai ver",
  "help.term.preview.body":
    "Uma prévia das memórias que o seu agente de IA recebe ao começar a trabalhar, para você conferir se fazem sentido.",
  "help.term.forgetting.name": "Esquecimento",
  "help.term.forgetting.body":
    "Memórias antigas e nunca usadas vão se apagando aos poucos e ficam ocultas, para que as úteis fiquem em destaque. Nada se perde de imediato.",
  "help.term.apiKey.name": "Chave de API",
  "help.term.apiKey.body":
    "Uma senha secreta que permite a uma ferramenta, como o Claude Code, ler e gravar suas memórias. Trate como uma senha.",
  "help.page.home":
    "O M3m0s é a sua memória de longo prazo para agentes de IA. Adicione notas, busque nelas e conecte seu agente para que ele lembre do que você contou.",
  "help.page.search":
    "Digite algumas palavras sobre o que você quer lembrar. Os resultados são as suas memórias salvas, da que mais combina para a que menos.",
  "help.page.add":
    "Salve algo que você quer que seus agentes lembrem. Curto e específico funciona melhor, uma ideia por vez.",
  "help.page.recent":
    "As memórias salvas mais recentemente. Use os filtros para restringir por tópico ou projeto.",
  "help.page.stats":
    "Números sobre a sua memória: quantas notas você tem, de onde vêm e o que o servidor está usando.",
  "help.page.insights":
    "Uma checagem de saúde das suas memórias. Mostra o que é antigo, não usado, duplicado ou de pouco valor, e nunca altera nada sozinha.",
  "help.page.jobs":
    "Rotinas que o servidor executa em segundo plano. Elas mantêm a memória em ordem, e você pode rodar qualquer uma agora.",
  "help.page.embedding":
    "Os embeddings fazem a busca entender o significado. Configure-os aqui com a chave de um provedor de IA; sem eles a busca só encontra palavras.",
  "help.page.preview":
    "Veja exatamente quais memórias um agente de IA receberia para um tópico, antes de conectar um.",
  "help.page.connect":
    "Dê às suas ferramentas de IA o endereço deste servidor e uma chave, e elas poderão ler e salvar memórias aqui.",
};
