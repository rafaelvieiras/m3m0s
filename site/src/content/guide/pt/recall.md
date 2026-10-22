---
title: Recuperação e ajustes
description: Como a recuperação por prompt decide o que injetar, quais ajustes existem e o que verificar quando uma memória aparece (ou não).
section: Reference
order: 11
---

A recuperação roda a cada prompt não trivial. Ela foi feita para ser discreta: quando nada é claramente relevante, nada é injetado.

## O que acontece a cada prompt

1. Prompts com menos de 20 caracteres e comandos de barra são ignorados, sem nenhuma chamada.
2. O prompt recebe embedding e é buscado no modo **vetorial**. Só a busca vetorial gera uma pontuação de cosseno que pode ser comparada a um limite.
3. Os resultados precisam atingir uma pontuação de cosseno de **0,60**. No máximo **3** entradas e cerca de **500** tokens estimados são mantidos.
4. Um filtro de tópico descarta resultados que não compartilham nenhum termo com o prompt. Nomes com erro de digitação são casados de forma aproximada, e os termos da consulta são corrigidos no servidor.
5. Um resultado cujo domínio é **de outro projeto** (`code:<outra-pasta>`) precisa superar o limite mais 0,10, para que uma nota vagamente relacionada de um repositório não relacionado não vaze. Domínios fixos que não são de código, como o usado pelo provedor do Hermes, mantêm o limite simples.
6. Memórias já injetadas na sessão são lembradas e não se repetem.

Entradas brutas de fallback nunca são injetadas. A chamada tem timeout de 4 segundos, normalmente bem abaixo de um segundo, e toda falha é silenciosa.

Os clientes que anexam o bloco por conta própria (pi, opencode) o envolvem como memória recuperada, com uma nota de que não é entrada do usuário, como faz o Hermes.

## Ajustes

| Variável | Efeito |
|---|---|
| `M3M0S_PROMPT_INJECT=off` | Desliga a recuperação por prompt. A captura continua funcionando. |
| `M3M0S_PROMPT_MIN_SCORE` | Limite de cosseno, padrão `0.60`. Aumente para ter menos resultados, porém mais seguros. |
| `M3M0S_PROMPT_FOREIGN_BOOST=off` | Remove os 0,10 extras para memórias de outros projetos. |

Defina-as no ambiente da ferramenta que dispara o hook.

## Está funcionando?

Toda injeção é registrada em `~/.m3m0s/hooks/claude-code.log`, com a pontuação de cada resultado. Em uma amostra de produção, prompts relevantes pontuaram de 0,66 a 0,68 no topo e os não relacionados chegaram a cerca de 0,40, então 0,60 os separa com pouca margem. Se houver ruído, aumente um pouco o limite; se memórias úteis nunca aparecerem, diminua um pouco e acompanhe o log.

Um prompt como "pode seguir" não tem tópico próprio, então não recupera nada. A recuperação não reescreve consultas a partir da conversa.

## Domínios

As memórias capturadas são arquivadas sob um **domínio**, por padrão `code:<nome da pasta>` do diretório onde a sessão começou. O domínio, e não o projeto, é o escopo real: é o que a recuperação usa para distinguir o seu projeto de outro, e o que você passa para `--domain` na CLI. Defina `M3M0S_HOOK_DOMAIN` para substituí-lo em uma ferramenta.
