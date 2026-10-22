---
title: opencode
description: Um plugin para o opencode que anexa memórias relevantes a cada turno e captura as sessões quando ficam ociosas.
section: Connect your tools
order: 8
---

O plugin do opencode dá ao [opencode](https://opencode.ai) o mesmo ciclo de memória dos hooks do Claude Code.

- **Recuperação:** cada mensagem do usuário é buscada no m3m0s, e os resultados relevantes são anexados ao prompt de sistema daquele turno.
- **Captura:** em `session.idle` (como `Stop`) e no encerramento (`SessionEnd`), o texto da sessão é gravado em `~/.m3m0s/hooks/opencode/<session>.jsonl` e entregue ao `m3m0s hook claude-code`. As memórias carregam `source: "opencode"`.

## Instalação

1. Instale o binário e guarde as credenciais: [CLI e credenciais](/pt/guide/cli/). Defina `M3M0S_BIN` se o binário não estiver em `~/go/bin/m3m0s` nem no `PATH`.
2. Adicione o pacote npm ao array `plugin` do `opencode.json`. O opencode o instala ao iniciar:

```json
{
  "plugin": ["m3m0s-opencode"]
}
```

### Instalação manual

Ou coloque o arquivo na pasta de plugins do opencode:

```sh
mkdir -p ~/.config/opencode/plugins
curl -fsSL https://raw.githubusercontent.com/rafaelvieiras/m3m0s/main/integrations/opencode/m3m0s-memory.js \
  -o ~/.config/opencode/plugins/m3m0s-memory.js
```

Depois adicione-o ao `opencode.json`:

```json
{
  "plugin": ["./plugins/m3m0s-memory.js"]
}
```

## Opcional: MCP somente leitura

Para que o agente também possa buscar explicitamente:

```json
{
  "mcp": {
    "m3m0s": {
      "type": "local",
      "enabled": true,
      "command": ["sh", "-c", "set -a; . \"$HOME/.m3m0s/hook.env\"; exec m3m0s mcp --read-only"]
    }
  }
}
```

## Solução de problemas

As falhas são silenciosas e nunca bloqueiam o opencode. O log fica em `~/.m3m0s/hooks/claude-code.log`. Execute `m3m0s doctor` para verificar as credenciais e a conexão.
