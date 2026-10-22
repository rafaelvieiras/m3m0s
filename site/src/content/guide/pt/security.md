---
title: Segurança
description: O que o m3m0s armazena, como ele protege esses dados e como executá-lo com segurança.
section: Reference
order: 12
---

O m3m0s armazena tudo o que você envia, incluindo transcrições de sessões, e você o configura com chaves de API de provedores. Trate o servidor e seus backups como qualquer outro sistema que guarde esse tipo de texto.

## O que protege seus dados

- **Chaves de API** protegem a API HTTP, a ponte MCP e a CLI. As contas da WebUI têm nome de usuário e senha, e a redefinição é um link de uso único válido por 30 minutos.
- **A ocultação de segredos** roda na ingestão e novamente antes de o texto ser enviado a um provedor de extração. É feita no melhor esforço: não consegue pegar todos os formatos.
- **`m3m0s maintenance redact-scan`** relata segredos armazenados antes de existir uma regra de ocultação. `--apply` reoculta os blobs de origem armazenados. Por padrão, é apenas uma simulação.
- **O modo MCP somente leitura** impede que agentes reescrevam a memória. É uma conveniência, não uma fronteira: qualquer um com a chave de API pode chamar a API HTTP.
- **Os provedores veem o que você envia a eles.** O texto vai para os endpoints de embedding, extração e chat que você configurar, e para nenhum outro lugar por padrão.

## Execute com segurança

1. Coloque o servidor atrás de TLS e de uma fronteira de rede confiável antes de expô-lo. Uma rede privada ou VPN é melhor que a internet aberta.
2. Mantenha o `.env` e o `~/.m3m0s/hook.env` fora do controle de versão, com permissão `600` para o segundo.
3. Trate os backups e o volume de dados como sensíveis.
4. Use um provedor de embedding e de extração em que você confie para o texto que seus agentes veem, ou aponte `M3M0S_OPENAI_BASE_URL` para um endpoint local.

## Relatando uma vulnerabilidade

Não abra uma issue pública. Use o relato privado de vulnerabilidades do GitHub: abra a aba **Security** do repositório e escolha **Report a vulnerability**. Inclua a versão afetada, como você executa o m3m0s e os passos para reproduzir, usando apenas dados sintéticos. Veja o [`SECURITY.md`](https://github.com/rafaelvieiras/m3m0s/blob/main/SECURITY.md) para o escopo e as versões suportadas.

Nunca publique chaves de API, transcrições, conteúdo real de memória, arquivos de banco de dados ou nomes de host internos em issues ou logs públicos. Se um segredo vazou, troque-o imediatamente.
