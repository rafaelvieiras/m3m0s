---
title: Configuração
description: Variáveis de ambiente do servidor e dos clientes.
section: Deploy
order: 4
---

O servidor é configurado com variáveis de ambiente (ou com as flags equivalentes de `m3m0s serve`). Com Docker Compose, coloque-as no `.env`.

## Servidor

| Variável | Padrão | Finalidade |
|---|---|---|
| `OPENAI_API_KEY` | nenhum (só palavras-chave) | Chave para embeddings (`text-embedding-3-small`). Sem ela, o servidor roda só com palavras-chave: busca full-text e deduplicação por correspondência exata. |
| `M3M0S_OPENAI_BASE_URL` | OpenAI | Endpoint alternativo de embeddings compatível com a OpenAI. |
| `M3M0S_PORT` | `19234` | Porta HTTP. |
| `M3M0S_DATA_DIR` | `~/.m3m0s/data` (`/data` na imagem) | Onde fica o banco SQLite. |
| `M3M0S_HOST_PORT` | `19234` | Porta do host publicada pelo `compose.yaml`. |
| `M3M0S_ADD_SYNC_TIMEOUT` | `300ms` | Quanto tempo o `add` espera antes de responder `202` e terminar em segundo plano. |
| `GOGC` | `50` | Alvo do GC do Go. Valores menores trocam CPU por um pico de memória menor. |
| `GOMEMLIMIT` | não definido | Limite suave de memória opcional, por exemplo `600MiB`. |

## Extração de fatos e chat (opcional)

A extração transforma uma entrada em vários fatos atômicos curtos. Ela fica desligada até você definir uma chave.

| Variável | Padrão | Finalidade |
|---|---|---|
| `M3M0S_EXTRACT_API_KEY` | nenhum (desligado) | Ativa a extração de fatos por LLM. |
| `M3M0S_EXTRACT_BASE_URL` | `https://openrouter.ai/api/v1` | Endpoint compatível com a OpenAI para a extração. |
| `M3M0S_EXTRACT_MODEL` | `deepseek/deepseek-v4-flash` | Modelo de extração. |
| `M3M0S_CHAT_API_KEY` | chave da extração | Ativa o chat da WebUI. |
| `M3M0S_CHAT_BASE_URL`, `M3M0S_CHAT_MODEL` | OpenRouter, `deepseek/deepseek-v4-flash` | Provedor e modelo do chat. |

## Tarefas em segundo plano

| Variável | Padrão | Finalidade |
|---|---|---|
| `M3M0S_RETRO_EXTRACT_INTERVAL` | `2h` | Intervalo da tarefa de extração retroativa. |
| `M3M0S_FORGET_SWEEP_INTERVAL` | `12h` | Intervalo da varredura de esquecimento. |
| `M3M0S_RETENTION_MODEL` | `legacy` | Pontuação usada pela varredura: `legacy` (uma única meia-vida de 30 dias) ou `per-kind` (meia-vida conforme a natureza do fato, opt-in). |
| `M3M0S_REEMBED_BATCH`, `M3M0S_REEMBED_RPS` | `64`, `5` | Tamanho do lote e taxa de requisições da tarefa de reembedding. |
| `M3M0S_JOBS_TICK` | `30s` | Pulso do agendador. |
| `M3M0S_INSIGHTS_CPU_PERCENT` | `60` | Ciclo de trabalho de CPU da análise de saúde da memória. |
| `M3M0S_INSIGHTS_MAX_MINUTES` | `30` | Limite de tempo real dessa análise. |

## Filtro de ingestão

A captura automática descarta ou marca o boilerplate de ferramentas antes de armazená-lo.

| Variável | Padrão | Finalidade |
|---|---|---|
| `M3M0S_FILTER_MODE` | `default` | Modo global: `default`, `off`, `tag` ou `drop`. |
| `M3M0S_FILTER_MODES` | nenhum | Modos por regra que prevalecem sobre o global, por exemplo `diff-trivia-fact=drop`. |
| `M3M0S_FILTER_RULES` | nenhum | Arquivo JSON de regras que substituem, adicionam ou desativam as embutidas. |

## Clientes

Estas são lidas pela CLI, pela ponte MCP, pelo watcher e pelos hooks.

| Variável | Padrão | Finalidade |
|---|---|---|
| `M3M0S_URL` | `http://localhost:19234` | URL do servidor. |
| `M3M0S_API_KEY` | nenhum | Chave de API. |
| `M3M0S_HOOK_DOMAIN` | derivado do diretório de trabalho | Domínio sob o qual as memórias capturadas são arquivadas. |

As variáveis de ajuste da recuperação estão na página [Recuperação e ajustes](/pt/guide/recall/).

## Provedor de embeddings em tempo de execução

O modelo, o endpoint, as dimensões e a chave de embeddings também podem ser alterados sem reiniciar, pela WebUI (página Embedding) ou pela CLI:

```sh
m3m0s embedding show
m3m0s embedding estimate --model text-embedding-3-large --dimensions 1024
echo "$NEW_KEY" | m3m0s embedding set --model text-embedding-3-large --api-key-stdin
m3m0s embedding reembed start --watch
```

A chave nunca é aceita como argumento e nunca é impressa. Se uma mudança deixar vetores armazenados em outro modelo ou tamanho, o `set` mostra quantos são e um custo aproximado, e pede a sua confirmação.
