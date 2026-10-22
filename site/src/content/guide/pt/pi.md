---
title: pi
description: Uma extensão para o agente de código pi que recupera memória antes de cada execução e captura as sessões depois.
section: Connect your tools
order: 7
---

A extensão do pi dá ao pi o mesmo ciclo de memória dos hooks do Claude Code, chamando o binário `m3m0s`.

- **Recuperação** (`before_agent_start`): executa `m3m0s hook claude-code --inject` com o prompt e anexa as memórias relevantes como uma mensagem personalizada oculta. O bloco é envolvido em `<memory-context>` com uma nota de que se trata de memória recuperada e não de uma nova entrada do usuário.
- **Captura** (`agent_end`, `session_shutdown`): executa `m3m0s hook claude-code` sobre o arquivo de sessão do pi. As memórias são armazenadas com `source: "pi"`.

Nada é injetado na compactação. A recuperação acontece por prompt.

## Instalação

1. Instale o binário e guarde as credenciais: [CLI e credenciais](/pt/guide/cli/). A extensão procura o `m3m0s` primeiro em `~/go/bin/m3m0s` e depois no `PATH`.
2. Instale o pacote pelo npm:

```sh
pi install npm:m3m0s-pi
```

3. Reinicie o pi. Defina `M3M0S_BIN` se o binário estiver em outro lugar.

### Instalação manual

Em vez disso, copie a extensão para a pasta de extensões do pi:

```sh
mkdir -p ~/.pi/agent/extensions
curl -fsSL https://raw.githubusercontent.com/rafaelvieiras/m3m0s/main/integrations/pi/m3m0s-memory.ts \
  -o ~/.pi/agent/extensions/m3m0s-memory.ts
```


Depois reinicie o pi.

## Opcional: MCP somente leitura

Registre o servidor em `~/.pi/agent/mcp.json`:

```json
{
  "m3m0s": {
    "command": "sh",
    "args": ["-c", "set -a; . \"$HOME/.m3m0s/hook.env\"; exec m3m0s mcp --read-only"]
  }
}
```

## Solução de problemas

As falhas são silenciosas e nunca bloqueiam o pi. Leia `~/.m3m0s/hooks/claude-code.log` e execute `m3m0s doctor`. Se aparecer uma memória de outro projeto, veja [Recuperação e ajustes](/pt/guide/recall/).
