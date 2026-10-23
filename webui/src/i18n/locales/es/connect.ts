import type { Messages } from "../../types";

export const connect: Messages<typeof import("../en/connect").connect> = {
  "connect.title": "Conecta tu agente",
  "connect.subtitle": "Apunta tus herramientas de IA a este servidor para que lean y guarden tus memorias.",
  "connect.step1.title": "1. La dirección de este servidor",
  "connect.step1.body": "Tus herramientas hablan con M3m0s en esta dirección.",
  "connect.step2.title": "2. Una clave de API",
  "connect.step2.body":
    "Una clave es la contraseña que una herramienta usa para llegar a tus memorias. M3m0s imprimió tu primera clave en el registro del servidor la primera vez que arrancó.",
  "connect.step2.howto":
    "Si ejecutas M3m0s con Docker, este comando la encuentra. Busca una línea que empiece por m3m0s_ y pégala donde los fragmentos de abajo dicen {placeholder}:",
  "connect.step2.warn":
    "Trata la clave como una contraseña: nunca la compartas ni la pegues en un lugar público. Crear claves adicionales desde esta página aún no es posible.",
  "connect.step3.title": "3. Pega la configuración en tu herramienta",
  "connect.step3.body": "Elige tu herramienta y copia el fragmento.",
  "connect.tool.claude": "Claude Code",
  "connect.tool.pi": "pi",
  "connect.tool.opencode": "opencode",
  "connect.snippet.claude":
    "Ejecuta esto una vez en tu terminal para añadir M3m0s como herramienta de memoria:",
  "connect.snippet.pi": "Añade esto a la configuración MCP de pi:",
  "connect.snippet.opencode": "Añade esto a la configuración de opencode (opencode.json):",
  "connect.copy": "Copiar",
  "connect.copied": "Copiado",
  "connect.test.title": "4. Mira lo que verá tu agente",
  "connect.test.body": "Una vez conectado, previsualiza las memorias que tu agente recibe para un tema.",
  "connect.test.action": "Abrir la vista previa",
  "connect.placeholder": "TU_CLAVE_DE_API",
  "connect.requires":
    "Estas configuraciones necesitan el programa m3m0s instalado en el mismo equipo que tu herramienta.",
};
