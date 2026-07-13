# WhatsApp AI Assistant

Assistente pessoal com IA via WhatsApp, construído para estudar AWS, System Design e arquiteturas orientadas a eventos mantendo custo mensal abaixo de `R$20`.

## Princípios

- `serverless first`
- zero infraestrutura ligada continuamente
- free tier sempre que possível
- domínio desacoplado de AWS e do provider de IA
- infraestrutura 100% versionada com OpenTofu
- Cloudflare DNS opcional para domínios públicos

## Estrutura

```text
.
├── apps
│   ├── cli
│   ├── webhook
│   └── worker
├── docs
│   ├── adr
│   └── cloudflare-dns.md
├── infra
│   ├── modules
│   └── opentofu
└── packages
    ├── domain
    ├── llm
    ├── memory
    ├── orchestrator
    ├── shared
    └── tools
```

## Fluxo

```text
WhatsApp -> API Gateway -> Lambda Webhook -> Orchestrator
                                        |-> Memory
                                        |-> Tool Registry
                                        |-> LLM Client
```

## Pacotes

- `packages/domain`: contratos e regras centrais do domínio.
- `packages/orchestrator`: coordena memória, tools e LLM.
- `packages/llm`: abstrações do provider e adapter local de desenvolvimento.
- `packages/memory`: contratos e implementações de memória.
- `packages/tools`: registry e contratos de tools.
- `apps/webhook`: handler Lambda e runner local para o webhook do WhatsApp.
- `apps/worker`: entrypoint para processamento assíncrono futuro.
- `apps/cli`: fluxo local para exercitar o orchestrator sem WhatsApp.
- `infra/modules/cloudflare_dns`: módulo opcional para registros DNS no Cloudflare.

## Estado atual

Esta base entrega:

- monorepo inicial em Go
- contratos normalizados do domínio
- orchestrator com fluxo explícito
- adapters in-memory para memória e tools
- handler Lambda com runner local de desenvolvimento
- base inicial de OpenTofu para Lambda, API Gateway, DynamoDB e Secrets Manager
- módulo opcional de Cloudflare DNS, desligado por padrão
- ADRs iniciais

## Ambiente local

```bash
cp .env.example .env
make build
make run-webhook
```

Para desenvolvimento local, `make run-webhook` sobe um servidor HTTP que reaproveita a mesma app do handler Lambda.
Em produção, o entrypoint é `apps/webhook/cmd/lambda`.

> NixOS: um `flake.nix` está disponível para instalar as ferramentas necessárias (go, tofu, etc.) via `nix develop`.

## Cloudflare DNS

Quando você quiser um domínio público gerenciado via Cloudflare, habilite o módulo opcional em `infra/opentofu/environments/dev`.
Ele fica desligado por padrão, então o fluxo AWS-only continua barato e simples.

Detalhes estão em [docs/cloudflare-dns.md](/home/emerson/dev/emerbot/docs/cloudflare-dns.md:1).

## Próximos passos

1. Implementar adapter real do WhatsApp.
2. Implementar adapter real do Gemini respeitando `llm.Client`.
3. Persistir mensagens e memórias no DynamoDB.
4. Empacotar `apps/webhook/cmd/lambda` para deploy via OpenTofu.
