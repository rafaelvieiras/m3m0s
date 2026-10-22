---
title: CLI e credenciais
description: Instale o binário m3m0s na máquina onde seus agentes rodam, guarde as credenciais uma única vez e verifique se a captura funciona.
section: Connect your tools
order: 5
---

Toda integração reutiliza o binário `m3m0s`, então instale-o em cada máquina onde você executa um agente. É o mesmo binário do servidor.

## Instale o binário

Baixe o arquivo para a sua arquitetura na [página de releases](https://github.com/rafaelvieiras/m3m0s/releases) (Linux `amd64`, `arm64`, `armv7`) e coloque o `m3m0s` no seu `PATH`. Ou compile com Go:

```sh
go install github.com/rafaelvieiras/m3m0s/cmd/m3m0s@latest
```

Isso instala em `~/go/bin/m3m0s`. Garanta que esse diretório esteja no `PATH` da ferramenta que dispara os hooks.

## Guarde as credenciais

Hooks e plugins leem a URL do servidor e a chave de API do ambiente e, na falta delas, de `~/.m3m0s/hook.env`. Crie esse arquivo uma vez e todas as integrações o usarão:

```sh
mkdir -p ~/.m3m0s
cat > ~/.m3m0s/hook.env <<'ENV'
M3M0S_URL=https://memory.example.com
M3M0S_API_KEY=<your API key>
ENV
chmod 600 ~/.m3m0s/hook.env
```

Uma linha opcional `M3M0S_HOOK_DOMAIN` altera o domínio sob o qual as memórias capturadas são arquivadas. Sem ela, o domínio é derivado do diretório de trabalho (`code:<nome da pasta>`).

## Verifique se funciona

```sh
m3m0s doctor
```

O `doctor` verifica as credenciais, a conexão com o servidor, o registro dos hooks do Claude Code e o spool de capturas que não puderam ser enviadas. Ele sai com status 1 quando algo falha e mostra como corrigir. `m3m0s doctor --replay` reenvia o spool pendente.

Os próprios hooks sempre saem com 0, porque nunca devem quebrar o agente hospedeiro. O log deles fica em `~/.m3m0s/hooks/claude-code.log`.

## Comandos do dia a dia

```sh
m3m0s add "Deploys go through the staging branch first" --domain code:myproject
m3m0s search "how do we deploy?" --domain code:myproject
m3m0s search "retry policy" --kind rule
m3m0s recent --limit 10
m3m0s get <id>
m3m0s update <id> --kind gotcha
m3m0s stats
```

A lista completa de operações, para MCP, CLI e HTTP, está em `docs/api-contract.md` no repositório.

## Observe a documentação de um projeto

```sh
m3m0s watch ~/projects/myproject
```

O watcher indexa `AGENTS.md`, `CLAUDE.md`, `MEMORY.md` e `docs/**/*.md` e os reindexa quando mudam.
