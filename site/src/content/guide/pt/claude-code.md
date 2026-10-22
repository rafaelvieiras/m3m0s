---
title: Claude Code
description: Capture sessões e recupere memória a cada prompt no Claude Code, com hooks e um servidor MCP opcional somente leitura.
section: Connect your tools
order: 6
---

No Claude Code, o m3m0s é um conjunto de hooks que executam `m3m0s hook claude-code`. Instale primeiro o binário e guarde as credenciais, como descrito em [CLI e credenciais](/pt/guide/cli/).

## O que os hooks fazem

| Evento | Função |
|---|---|
| `SessionStart` | Injeta um resumo das memórias recentes. |
| `UserPromptSubmit` | Duas entradas. Uma síncrona, com `--inject`, busca na memória pelo prompt e injeta os resultados relevantes. Uma assíncrona faz a captura. |
| `Stop`, `PreCompact`, `SessionEnd` | Capturam a parte nova da transcrição (`PreCompact` e `SessionEnd` podem ser assíncronos). |
| `PostToolUse` | Rede de segurança opcional. Não faz nada até a transcrição crescer. |

Hooks assíncronos não conseguem injetar contexto, e por isso a entrada com `--inject` precisa ser síncrona.

## Instale o plugin

O plugin reúne todos os hooks abaixo e o servidor MCP somente leitura. Instale antes o binário e o `~/.m3m0s/hook.env` e, depois, no Claude Code:

```
/plugin marketplace add rafaelvieiras/m3m0s
/plugin install m3m0s-memory@m3m0s
```

No shell, o equivalente é `claude plugin marketplace add rafaelvieiras/m3m0s` seguido de `claude plugin install m3m0s-memory@m3m0s`. Se você já configurou esses hooks no `settings.json`, remova-os primeiro para que nada rode em dobro. O plugin cobre também o servidor MCP, então pule a seção opcional abaixo.

## Ou configure os hooks manualmente

Se preferir não usar o plugin, adicione isto ao `~/.claude/settings.json`:

```json
{
  "hooks": {
    "SessionStart": [
      { "hooks": [{ "type": "command", "timeout": 15,
        "command": "m3m0s hook claude-code 2>>~/.m3m0s/hooks/claude-code.log || true" }] }
    ],
    "UserPromptSubmit": [
      { "hooks": [{ "type": "command", "timeout": 60, "async": true,
        "command": "m3m0s hook claude-code 2>>~/.m3m0s/hooks/claude-code.log || true" }] },
      { "hooks": [{ "type": "command", "timeout": 10,
        "command": "m3m0s hook claude-code --inject 2>>~/.m3m0s/hooks/claude-code.log || true" }] }
    ],
    "Stop": [
      { "hooks": [{ "type": "command", "timeout": 60,
        "command": "m3m0s hook claude-code 2>>~/.m3m0s/hooks/claude-code.log || true" }] }
    ],
    "PreCompact": [
      { "hooks": [{ "type": "command", "timeout": 60, "async": true,
        "command": "m3m0s hook claude-code 2>>~/.m3m0s/hooks/claude-code.log || true" }] }
    ],
    "SessionEnd": [
      { "hooks": [{ "type": "command", "timeout": 60, "async": true,
        "command": "m3m0s hook claude-code 2>>~/.m3m0s/hooks/claude-code.log || true" }] }
    ]
  }
}
```

Crie o diretório de log uma vez com `mkdir -p ~/.m3m0s/hooks` e execute `m3m0s doctor` para confirmar que os eventos estão registrados.

## Opcional: servidor MCP somente leitura (só na instalação manual)

Com a captura e a recuperação automáticas, o agente só precisa buscar explicitamente de vez em quando. Registre o servidor MCP em modo somente leitura, para que ele não possa adicionar nem reescrever memórias:

```sh
claude mcp add m3m0s --scope user \
  -e M3M0S_URL=https://memory.example.com \
  -e M3M0S_API_KEY=<your API key> \
  -- m3m0s mcp --read-only
```

Veja [Clientes MCP](/pt/guide/mcp/) para conhecer as ferramentas que ele expõe.

## Verifique

Abra uma nova sessão em um projeto para o qual você tem memórias e pergunte sobre algo que decidiu antes. Uma memória correspondente é injetada antes de o modelo responder. Toda injeção é registrada em `~/.m3m0s/hooks/claude-code.log`.
