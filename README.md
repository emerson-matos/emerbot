# WhatsApp AI Assistant

Assistente pessoal com IA via WhatsApp, construído para estudar AWS, System Design e arquiteturas orientadas a eventos mantendo custo mensal abaixo de `R$20`.

## Princípios

- `serverless first`
- zero infraestrutura ligada continuamente
- free tier sempre que possível
- domínio desacoplado de AWS e do provider de IA
- infraestrutura 100% versionada com OpenTofu

## Estrutura

```text
.
├── apps
│   ├── cli
│   ├── webhook
│   └── worker
├── docs
│   └── adr
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

## Estado atual

Esta base entrega:

- monorepo inicial em Go
- contratos normalizados do domínio
- orchestrator com fluxo explícito
- adapters in-memory para memória e tools
- handler Lambda com runner local de desenvolvimento
- base inicial de OpenTofu para Lambda, API Gateway, DynamoDB e Secrets Manager
- ADRs iniciais

## Ambiente local

Em NixOS, o fluxo esperado e suportado é com `flake` + `direnv`.

```bash
cp .env.example .env
direnv allow
make build
make run-webhook
```

Para desenvolvimento local, `make run-webhook` sobe um servidor HTTP que reaproveita a mesma app do handler Lambda.
Em produção, o entrypoint é `apps/webhook/cmd/lambda`.

O `flake.nix` instala:

- `go`
- `gopls`
- `gofumpt`
- `golangci-lint`
- `opentofu`
- `awscli2`
- utilitários mínimos como `jq` e `zip`

## Próximos passos

1. Implementar adapter real do WhatsApp.
2. Implementar adapter real do Gemini respeitando `LLMClient`.
3. Persistir mensagens e memórias no DynamoDB.
4. Empacotar `apps/webhook/cmd/lambda` para deploy via OpenTofu.
