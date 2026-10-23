import type { Messages } from "../../types";

export const connect: Messages<typeof import("../en/connect").connect> = {
  "connect.title": "Conecte o seu agente",
  "connect.subtitle":
    "Aponte suas ferramentas de IA para este servidor para que elas leiam e salvem suas memórias.",
  "connect.step1.title": "1. O endereço deste servidor",
  "connect.step1.body": "Suas ferramentas falam com o M3m0s neste endereço.",
  "connect.step2.title": "2. Uma chave de API",
  "connect.step2.body":
    "Uma chave é a senha que uma ferramenta usa para chegar às suas memórias. O M3m0s imprimiu a sua primeira chave no log do servidor na primeira vez que iniciou.",
  "connect.step2.howto":
    "Se você roda o M3m0s com Docker, este comando a encontra. Procure uma linha que comece com m3m0s_ e cole-a onde os trechos abaixo dizem {placeholder}:",
  "connect.step2.warn":
    "Trate a chave como uma senha: nunca a compartilhe nem a cole em lugar público. Criar chaves extras por esta página ainda não é possível.",
  "connect.step3.title": "3. Cole a configuração na sua ferramenta",
  "connect.step3.body": "Escolha a sua ferramenta e copie o trecho.",
  "connect.tool.claude": "Claude Code",
  "connect.tool.pi": "pi",
  "connect.tool.opencode": "opencode",
  "connect.snippet.claude":
    "Rode isto uma vez no terminal para adicionar o M3m0s como ferramenta de memória:",
  "connect.snippet.pi": "Adicione isto às configurações de MCP do pi:",
  "connect.snippet.opencode": "Adicione isto à configuração do opencode (opencode.json):",
  "connect.copy": "Copiar",
  "connect.copied": "Copiado",
  "connect.test.title": "4. Veja o que o seu agente vai ver",
  "connect.test.body": "Depois de conectar, visualize as memórias que o seu agente recebe para um tópico.",
  "connect.test.action": "Abrir a prévia",
  "connect.placeholder": "SUA_CHAVE_DE_API",
  "connect.requires":
    "Estas configurações precisam do programa m3m0s instalado no mesmo computador da sua ferramenta.",
};
