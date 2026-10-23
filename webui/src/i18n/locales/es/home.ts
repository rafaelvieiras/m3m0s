import type { Messages } from "../../types";

export const home: Messages<typeof import("../en/home").home> = {
  "home.title": "Te damos la bienvenida a M3m0s",
  "home.subtitle": "Memoria a largo plazo para ti y tus agentes de IA.",
  "home.count.one": "Tienes {count} memoria guardada.",
  "home.count.other": "Tienes {count} memorias guardadas.",
  "home.docsNote":
    "M3m0s también incluye {count} páginas de documentación integrada. Están ocultas en tus listas.",
  "home.checklist.title": "Primeros pasos",
  "home.checklist.progress": "{done} de {total} completados",
  "home.checklist.allDone": "Todo listo. Tus agentes ya pueden recordar cosas entre sesiones.",
  "home.step.account.title": "Crea tu cuenta",
  "home.step.account.body": "Hecho: has iniciado sesión.",
  "home.step.memory.title": "Guarda tu primera memoria",
  "home.step.memory.body": "Anota algo que valga la pena recordar, como una decisión o una preferencia.",
  "home.step.memory.action": "Añadir una memoria",
  "home.step.agent.title": "Conecta tu agente de IA",
  "home.step.agent.body": "Dale a Claude Code u otra herramienta la dirección de este servidor y una clave.",
  "home.step.agent.action": "Conectar un agente",
  "home.step.search.title": "Activa la búsqueda por significado",
  "home.step.search.body":
    "Opcional. Sin ella, la búsqueda solo encuentra las palabras exactas que escribes.",
  "home.step.search.action": "Configurar embeddings",
  "home.done": "Hecho",
  "home.quick.title": "¿Qué quieres hacer?",
  "home.quick.search": "Encontrar algo que guardaste",
  "home.quick.add": "Guardar una memoria nueva",
  "home.quick.connect": "Conectar una herramienta de IA",
  "home.empty.title": "Tu memoria está vacía",
  "home.empty.body":
    "Una memoria es una nota corta, como \"Elegimos PostgreSQL como base de datos\". Guarda unas cuantas y tus agentes empezarán a usarlas.",
  "home.empty.example": "Ejemplo: \"Prefiero respuestas cortas, con el código primero.\"",
};
