---
title: Início rápido
description: Execute o servidor com Docker Compose, crie sua conta e armazene sua primeira memória em cerca de cinco minutos.
section: Start
order: 2
---

Você precisa do Docker com Compose v2.24 ou superior e de uma chave de API compatível com a OpenAI para os embeddings. Sem a chave, o servidor continua funcionando, em modo somente por palavras-chave (busca full-text e deduplicação por correspondência exata).

## 1. Inicie o servidor

```sh
git clone https://github.com/rafaelvieiras/m3m0s.git
cd m3m0s
cp .env.example .env        # set OPENAI_API_KEY
docker compose up -d
curl http://localhost:19234/health
docker compose logs         # the first start prints a generated API key
```

> Guarde a chave de API do log. Ela é exibida uma única vez e não é impressa novamente.

## 2. Crie sua conta

Abra `http://localhost:19234/`. Uma instância nova executa a configuração inicial: crie a primeira conta com um nome de usuário e uma senha, sem precisar de chave. Faça isso imediatamente se a instância estiver acessível pela internet, porque quem abri-la primeiro toma posse dela. A partir daí, você entra com essa conta.

As contas são para pessoas que usam a WebUI. Hooks, MCP e a CLI continuam usando chaves de API.

## 3. Instale a CLI

A CLI é o mesmo binário do servidor. Baixe um arquivo para Linux (`amd64`, `arm64` ou `armv7`) na [página de releases](https://github.com/rafaelvieiras/m3m0s/releases), ou compile:

```sh
go install github.com/rafaelvieiras/m3m0s/cmd/m3m0s@latest
```

Aponte-a para o servidor:

```sh
export M3M0S_URL=http://localhost:19234
export M3M0S_API_KEY=<the key from the first start>

m3m0s add "We use SQLite with versioned migrations" --domain code:myproject
m3m0s search "which database do we use?" --domain code:myproject
```

## 4. Conecte uma ferramenta

Escolha a ferramenta que você usa e siga a página dela: [Claude Code](/pt/guide/claude-code/), [pi](/pt/guide/pi/), [opencode](/pt/guide/opencode/), [Hermes](/pt/guide/hermes/) ou [um cliente MCP](/pt/guide/mcp/). Todas leem as mesmas duas configurações, `M3M0S_URL` e `M3M0S_API_KEY`.

## Esqueceu a senha?

`m3m0s user reset <username>` imprime um link de uso único, válido por 30 minutos, que abre uma página para escolher uma nova senha. Execute com `M3M0S_URL` e `M3M0S_API_KEY` definidos, ou dentro do container. `m3m0s user add`, `list` e `delete` gerenciam outras contas, e todas compartilham a mesma memória.
