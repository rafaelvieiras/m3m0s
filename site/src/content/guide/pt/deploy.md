---
title: Deploy
description: Execute o m3m0s em um servidor com Docker Compose ou com o binário puro, atrás de TLS, com backups e atualizações.
section: Deploy
order: 3
---

O m3m0s armazena tudo o que você envia, incluindo transcrições de conversas. Coloque-o atrás de TLS e de uma fronteira de rede confiável antes de expô-lo à internet, e leia primeiro a página de [Segurança](/pt/guide/security/).

## Docker Compose

O repositório inclui um `compose.yaml` que constrói a imagem a partir do `Dockerfile` da raiz (build multi-stage: WebUI com Vite e, depois, um binário Go estático com a WebUI embutida). A imagem é Go puro, sem cgo, então também compila e roda em placas ARM pequenas.

```sh
cp .env.example .env
docker compose up -d --build
```

- A porta publicada no host é `M3M0S_HOST_PORT` (padrão `19234`).
- Os dados ficam no volume nomeado `m3m0s-data`, montado em `/data`. Ele contém o banco SQLite e o arquivo da chave de API. Um volume nomeado (e não um bind mount) mantém o dono correto para o usuário não-root dentro da imagem.
- A imagem tem um healthcheck embutido em `GET /health`.
- Os segredos vêm do `.env`. Mantenha-o fora do controle de versão.

As imagens também são publicadas em `ghcr.io/rafaelvieiras/m3m0s`, com tag por commit (`sha-<commit>`). Se você faz deploy por uma ferramenta de GitOps, aponte-a para essa imagem em vez de compilar no host.

## Atrás de um proxy reverso

Termine o TLS na frente do container. Com o Caddy, por exemplo:

```caddyfile
memory.example.com {
    reverse_proxy localhost:19234
}
```

Não é preciso mais nada: a WebUI e a API compartilham a mesma porta.

## Sem Docker

Baixe o arquivo para a sua arquitetura na [página de releases](https://github.com/rafaelvieiras/m3m0s/releases) e execute:

```sh
M3M0S_DATA_DIR=/var/lib/m3m0s OPENAI_API_KEY=sk-... m3m0s serve
```

Uma unit mínima do systemd:

```ini
[Unit]
Description=m3m0s memory server
After=network-online.target

[Service]
User=m3m0s
EnvironmentFile=/etc/m3m0s.env
Environment=M3M0S_DATA_DIR=/var/lib/m3m0s
ExecStart=/usr/local/bin/m3m0s serve
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

Para compilar a partir do código-fonte com a WebUI embutida, execute antes `npm ci && npm run build` em `webui/` e depois `go build ./cmd/m3m0s`.

## Hosts pequenos

Em uma placa com pouca RAM, defina um limite suave de memória para que o runtime do Go colete mais cedo, por exemplo `GOMEMLIMIT=600MiB`. Mantenha-o acima do tamanho em uso, que é de cerca de 250 MB para 75.000 memórias. O `GOGC` tem padrão 50, o que já troca um pouco de CPU por um pico menor.

## Backups

Todo o estado está no diretório de dados. Para uma cópia consistente, pare o container, arquive o volume e inicie-o novamente:

```sh
docker compose stop m3m0s
docker run --rm -v m3m0s-data:/data -v "$PWD":/backup alpine \
  tar czf /backup/m3m0s-data.tgz -C /data .
docker compose start m3m0s
```

A memória pode conter segredos armazenados antes de existir uma regra de ocultação. Trate os backups como sensíveis. `m3m0s maintenance redact-scan` relata esses casos, e `--apply` reoculta os blobs de origem armazenados (por padrão, apenas uma simulação).

## Atualização

```sh
git pull
docker compose up -d --build
```

O esquema do banco é migrado na inicialização por meio de migrações versionadas. Faça um backup antes quando pular várias versões.
