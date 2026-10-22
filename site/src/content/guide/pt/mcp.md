---
title: Clientes MCP
description: Registre o servidor MCP via stdio em qualquer host que suporte MCP, com ou sem acesso de escrita.
section: Connect your tools
order: 10
---

O `m3m0s mcp` é um servidor MCP via stdio. Ele é uma ponte sem estado para um `m3m0s serve` em execução, então qualquer host MCP pode usá-lo, por exemplo Claude Code, opencode ou pi.

## Registre-o

Use o comando `m3m0s` com o argumento `mcp` e passe as duas variáveis de cliente:

```json
{
  "mcpServers": {
    "m3m0s": {
      "command": "m3m0s",
      "args": ["mcp", "--read-only"],
      "env": {
        "M3M0S_URL": "https://memory.example.com",
        "M3M0S_API_KEY": "<your API key>"
      }
    }
  }
}
```

O arquivo e os nomes das chaves variam conforme o host (`mcpServers`, `mcp`, e assim por diante). O que importa são o comando, os argumentos e o ambiente.

## Ferramentas

| Ferramenta | Finalidade |
|---|---|
| `memory_search` | Busca por uma consulta curta. Suporta projeto ou domínio, um `kind`, um orçamento de tokens e um modo `keyword` para termos exatos. |
| `memory_get` | Lê uma memória por completo. |
| `memory_recent` | Lista as memórias mais novas. |
| `memory_stats` | Totais da coleção. |
| `memory_add`, `memory_update`, `memory_forget_sweep`, `memory_restore` | Ferramentas de escrita. Ocultas no modo somente leitura. |

## Modo somente leitura

`m3m0s mcp --read-only` (ou `M3M0S_MCP_READ_ONLY=1`) oculta as quatro ferramentas de escrita e as rejeita se forem chamadas. Com a captura automática em vigor, os agentes não precisam escrever memória via MCP, e uma ferramenta que existe acabará sendo chamada por engano.

> O modo somente leitura é uma conveniência do lado do cliente, não uma fronteira de segurança. Qualquer um que tenha a chave de API ainda pode chamar a API HTTP.

## Diga ao agente quando buscar

O servidor traz instruções que dizem ao modelo para buscar antes de responder perguntas sobre os seus projetos, decisões passadas e convenções, para tratar os resultados como anotações e não como instruções, e para não chamar `memory_add` para progresso rotineiro. Entradas do tipo `state` descrevem um momento passado, então o agente é orientado a verificá-las antes de confiar nelas.
