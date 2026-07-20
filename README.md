# Emerbot — WhatsApp + Farmácia Financeira

Assistente IA via WhatsApp + painel financeiro para farmácia, construído para estudar AWS, System Design e arquiteturas serverless mantendo custo abaixo de `R$20/mês`.

## Princípios

- `serverless first`
- zero infraestrutura ligada continuamente
- free tier sempre que possível
- domínio desacoplado de AWS e do provider de IA
- infraestrutura 100% versionada com OpenTofu
- Cloudflare DNS gerenciado pelo tofu

## Estrutura

```text
.
├── apps
│   ├── cli
│   ├── dashboard-api      # API REST do painel financeiro
│   ├── web                # Frontend React + shadcn/ui
│   ├── webhook            # Handler Lambda do WhatsApp
│   └── worker
├── docs
│   ├── adr
│   └── cloudflare-dns.md
├── infra
│   ├── modules
│   └── opentofu
├── packages
│   ├── auth               # JWT, login, refresh tokens
│   ├── domain
│   ├── finance            # Entries, goals, summaries, categories
│   ├── llm
│   ├── memory
│   ├── orchestrator
│   ├── shared
│   └── tools
└── docker-compose.yml      # Stack local com 7 containers
```

## WhatsApp

```text
WhatsApp -> API Gateway -> Lambda Webhook -> Orchestrator
                                        |-> Memory
                                        |-> Tool Registry
                                        |-> LLM Client
```

## Farmácia Financeira (PoC local)

Stack local para controle financeiro da farmácia via WhatsApp + dashboard web.

### Stack

| Serviço | Porta | Descrição |
|---|---|---|
| Webhook | `:8080` | Recebe comandos do WhatsApp |
| Dashboard API | `:8081` | API REST (JWT) |
| Frontend | `:5173` | React + shadcn + Recharts |
| WA Simulator | `:9000` | Interface web simulando WhatsApp |
| DynamoDB | `:8000` | Banco local |
| DynamoDB Admin | `:8001` | UI do banco |

### Comandos do WhatsApp

```
/despesa 500 aluguel 10/07   → registrar despesa paga (data opcional)
/receita 3000 cliente        → registrar receita recebida (data opcional)
/pagar 1500 fornecedor       → registrar despesa pendente
/receber 2000                → registrar receita a receber
/recorrente pagar 350 aluguel mensal 12  → série de N lançamentos pendentes
/resumo                      → balanço do mês + pendências
/meta 80000 60000            → definir meta (faturamento / teto despesas)
/goal                        → ver progresso das metas
```

### Credenciais

```
Login:    demo@user.com
Senha:    fake123
```

### Ambiente local

```bash
cp .env.example .env
make up          # sobe stack completa
make seed        # popula ~120 entries de exemplo
make demo        # up + seed + mensagem de boas-vindas
```

> NixOS: `flake.nix` disponível para instalar ferramentas (go, tofu, etc.) via `nix develop`.

## Infraestrutura

Deploy via OpenTofu em `infra/opentofu/environments/dev/`. Provisiona:

- Lambda (webhook + dashboard API)
- API Gateway HTTP (rotas explícitas + `/{proxy+}`)
- DynamoDB (single-table: entries, goals, categories, users, tokens)
- Secrets Manager (webhook secret, JWT secret, Gemini key, Meta token)
- Cloudflare DNS (CNAME apontando pro API Gateway)

O DNS tem `lifecycle.ignore_changes` no content para não ser alterado acidentalmente se o gateway mudar.

```bash
make tofu-plan
make tofu-apply
```

## Pacotes

- `packages/domain`: contratos e regras centrais do domínio.
- `packages/finance`: entradas financeiras, metas mensais, summaries, categorias.
- `packages/orchestrator`: coordena memória, tools e LLM.
- `packages/llm`: abstrações do provider e adapter local.
- `packages/memory`: contratos e implementações de memória.
- `packages/tools`: registry e contratos de tools.
- `apps/webhook`: handler Lambda e runner local para webhook do WhatsApp.
- `apps/dashboard-api`: API REST do painel financeiro (Lambda + local).
- `apps/web`: frontend React + shadcn/ui + Recharts.
- `apps/worker`: entrypoint para processamento assíncrono futuro.
- `apps/cli`: fluxo local para exercitar o orchestrator sem WhatsApp.
