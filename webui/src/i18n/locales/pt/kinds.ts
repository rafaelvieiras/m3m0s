import type { Messages } from "../../types";

export const kinds: Messages<typeof import("../en/kinds").kinds> = {
  "kinds.decision.label": "Decisão",
  "kinds.decision.desc": "Uma escolha que foi feita, de preferência com o motivo.",
  "kinds.rule.label": "Regra",
  "kinds.rule.desc": "Uma convenção ou instrução permanente que deve sempre ser seguida.",
  "kinds.preference.label": "Preferência",
  "kinds.preference.desc": "Algo de que você gosta ou que quer que seja feito de um certo jeito.",
  "kinds.gotcha.label": "Armadilha",
  "kinds.gotcha.desc": "Uma cilada ou comportamento pouco óbvio para evitar da próxima vez.",
  "kinds.procedure.label": "Passo a passo",
  "kinds.procedure.desc": "Etapas ou comandos para fazer alguma coisa.",
  "kinds.state.label": "Situação atual",
  "kinds.state.desc": "Algo verdadeiro agora que vai mudar, como o andamento ou a branch atual.",
  "kinds.noise.label": "Pouco valor",
  "kinds.noise.desc": "Enchimento que não vale a pena guardar.",
  "kinds.other.label": "Outro",
  "kinds.other.desc":
    "Qualquer coisa que não se encaixe nos outros tipos, ou que ainda não foi classificada.",
  "kinds.auto": "Deixar o M3m0s escolher",
  "kinds.autoDesc": "O M3m0s descobre o tipo para você.",
  "kinds.source.llm": "IA",
  "kinds.source.heuristic": "Regra automática",
  "kinds.source.manual": "Você",
  "kinds.source.none": "Sem classificação",
  "kinds.label": "Tipo de memória",
};
