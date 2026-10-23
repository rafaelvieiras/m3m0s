import type { Messages } from "../../types";

export const help: Messages<typeof import("../en/help").help> = {
  "help.about": "Acerca de esta página",
  "help.more": "¿Qué es esto?",
  "help.close": "Cerrar",
  "help.term": "Explicar: {term}",
  "help.term.memory.name": "Memoria",
  "help.term.memory.body":
    "Una nota corta que M3m0s guarda para ti y tus agentes de IA: una decisión, una regla, una preferencia o cualquier cosa que valga la pena recordar.",
  "help.term.kind.name": "Tipo de memoria",
  "help.term.kind.body":
    "Qué es la nota: una decisión, una regla, una preferencia, una trampa, un procedimiento, un estado u otra cosa. Ayuda a M3m0s a decidir qué vale la pena conservar.",
  "help.term.domain.name": "Tema",
  "help.term.domain.body":
    "El asunto al que pertenece una memoria, escrito como área:nombre, por ejemplo code:mi-app o personal:salud. Mantiene separadas las partes de tu vida.",
  "help.term.project.name": "Proyecto",
  "help.term.project.body":
    "Una etiqueta opcional que une una memoria a un proyecto. Las memorias sin proyecto las comparten todos.",
  "help.term.embedding.name": "Embeddings",
  "help.term.embedding.body":
    "Una forma de comparar el significado de los textos. Con los embeddings activados, buscar \"coche\" también puede encontrar una memoria sobre \"vehículo\".",
  "help.term.jobs.name": "Tareas",
  "help.term.jobs.body":
    "Rutinas que el servidor ejecuta en segundo plano, como limpiar memorias antiguas. Casi nunca hace falta tocarlas.",
  "help.term.insights.name": "Insights",
  "help.term.insights.body":
    "Un informe de salud de tus memorias: cuántas hay, qué antigüedad tienen y cuáles parecen duplicadas o de poco valor.",
  "help.term.preview.name": "Lo que verá el agente",
  "help.term.preview.body":
    "Una vista previa de las memorias que tu agente de IA recibe al empezar a trabajar, para que compruebes que tienen sentido.",
  "help.term.forgetting.name": "Olvido",
  "help.term.forgetting.body":
    "Las memorias antiguas y nunca usadas se van apagando y quedan ocultas, para que las útiles queden arriba. Nada se pierde de inmediato.",
  "help.term.apiKey.name": "Clave de API",
  "help.term.apiKey.body":
    "Una contraseña secreta que permite a una herramienta, como Claude Code, leer y escribir tus memorias. Trátala como una contraseña.",
  "help.page.home":
    "M3m0s es tu memoria a largo plazo para agentes de IA. Añade notas, búscalas y conecta tu agente para que recuerde lo que le contaste.",
  "help.page.search":
    "Escribe unas palabras sobre lo que quieres recordar. Los resultados son tus memorias guardadas, de mayor a menor coincidencia.",
  "help.page.add":
    "Guarda algo que quieras que tus agentes recuerden. Corto y específico funciona mejor, una idea cada vez.",
  "help.page.recent":
    "Tus memorias guardadas más recientemente. Usa los filtros para acotar por tema o proyecto.",
  "help.page.stats":
    "Números sobre tu memoria: cuántas notas tienes, de dónde vienen y qué está usando el servidor.",
  "help.page.insights":
    "Una revisión de salud de tus memorias. Muestra lo que es antiguo, no usado, duplicado o de poco valor, y nunca cambia nada por sí sola.",
  "help.page.jobs":
    "Rutinas que el servidor ejecuta en segundo plano. Mantienen ordenada tu memoria, y puedes ejecutar cualquiera ahora.",
  "help.page.embedding":
    "Los embeddings hacen que la búsqueda entienda el significado. Configúralos aquí con la clave de un proveedor de IA; sin ellos la búsqueda solo encuentra palabras.",
  "help.page.preview":
    "Mira exactamente qué memorias recibiría un agente de IA para un tema, antes de conectar uno.",
  "help.page.connect":
    "Dale a tus herramientas de IA la dirección de este servidor y una clave, y podrán leer y guardar memorias aquí.",
};
