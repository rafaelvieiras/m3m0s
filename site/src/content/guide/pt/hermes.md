---
title: Hermes
description: Um provedor de memória para agentes Hermes que recupera antes de cada turno, captura depois dele e adiciona uma ferramenta de busca somente leitura.
section: Connect your tools
order: 9
---

A integração com o Hermes é um **provedor de memória**. Ela reutiliza o binário `m3m0s`, então os filtros de relevância e a ocultação de segredos se comportam exatamente como nas outras ferramentas.

- **Recuperação** (`prefetch`): executa `m3m0s hook claude-code --inject` com a mensagem do usuário. O Hermes envolve o resultado no próprio bloco `<memory-context>`.
- **Captura** (`sync_turn` como `Stop`, `on_session_end` como `SessionEnd`): acrescenta cada turno a uma transcrição por sessão e a entrega ao `m3m0s hook claude-code`. As memórias carregam `source: "hermes"`, arquivadas em `M3M0S_HOOK_DOMAIN` (padrão `personal:hermes`).
- **Ferramenta** `m3m0s_search` (somente leitura): `query`, `limit`, `domain`, ou `recent` para listar as memórias mais novas de um domínio.

Turnos de cron e de webhook são ignorados. As falhas são silenciosas e nunca bloqueiam o agente.

## Instalação

O Hermes permite um único provedor de memória externo.

### Com a CLI do Hermes

O binário deve estar no `PATH` ou em `<plugin dir>/bin/m3m0s`:

```sh
hermes plugins install rafaelvieiras/m3m0s/integrations/hermes/m3m0s-memory
hermes memory setup   # pick m3m0s-memory
```

### Instalação manual

Os passos abaixo assumem que o agente roda em um container com um volume de dados montado em `<data>`.

1. Compile um binário estático para a plataforma do agente:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o m3m0s ./cmd/m3m0s
```

2. Copie o plugin e o binário para o volume de dados, com o usuário do agente como dono:

```sh
cp -r integrations/hermes/m3m0s-memory <data>/plugins/m3m0s-memory
install -m 755 m3m0s <data>/plugins/m3m0s-memory/bin/m3m0s
```

3. Adicione as credenciais em `<data>/.env`:

```sh
M3M0S_URL=https://memory.example.com
M3M0S_API_KEY=<your API key>
M3M0S_HOOK_DOMAIN=personal:hermes   # optional
```

4. Selecione o provedor em `<data>/config.yaml` e reinicie o agente:

```yaml
memory:
  provider: m3m0s-memory
```

`hermes memory status` deve mostrá-lo como ativo. O estado (checkpoints, spool, transcrições) fica em `<data>/m3m0s/`.
