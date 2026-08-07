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
│   ├── cli                # Exercita o orchestrator sem WhatsApp
│   ├── dashboard-api      # API REST do painel financeiro
│   ├── notifier           # Lambda agendada: resumo diário + contas do dia
│   ├── payment-importer   # Lambda de import de adquirentes (S3 → DynamoDB)
│   ├── web                # Frontend React + shadcn/ui
│   └── webhook            # Handler Lambda do WhatsApp
├── cenarios               # Extratos EDI do PagBank gravados (fixtures)
├── docs
│   ├── adr
│   ├── cloudflare-dns.md
│   └── payments-import.md
├── infra
│   ├── modules
│   └── opentofu
├── packages
│   ├── conversation       # Histórico curto da conversa (TTL)
│   ├── domain             # Contratos e regras centrais
│   ├── dynamostore        # Seam do DynamoDB + tabela fake para testes
│   ├── finance            # Entries, goals, summaries, categories, tools do agente
│   │   └── analytics      # Saúde, tendências, projeção, posição de caixa
│   ├── notifications      # Regras de alerta (contas vencendo / vencidas / meta)
│   ├── orchestrator       # Coordena memória, tools e o provider de IA
│   ├── payments           # Domínio de adquirentes + parser PagBank + importer
│   ├── shared             # Config, fuso da farmácia, id do ledger
│   ├── wasession          # Janela de 24h do WhatsApp (TTL)
│   └── whatsapp           # Cliente da Meta Cloud API
└── docker-compose.yml      # Stack local com 7 containers
```

## WhatsApp

```text
WhatsApp -> API Gateway -> Lambda Webhook -> Orchestrator -> Agente (Gemini)
                                                         |-> Histórico da conversa
                                                         |-> Finance tools
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

### Adquirentes (PagBank)

As vendas no cartão entram por import dos extratos EDI, sem digitação. `make demo`
já popula a página **Adquirentes** com os cenários gravados em `cenarios/`; para
importar de novo ou usar dados reais:

```sh
make seed-payments                       # cenários gravados → dynamodb-local
make import-pagbank DIR=~/extratos/hoje  # extratos reais → S3 → Lambda
```

É o mesmo script nos dois casos — só o transporte muda.
Detalhes em [`docs/payments-import.md`](docs/payments-import.md).

### Conversa no WhatsApp

Não há comandos. Escreva em português normal e o agente resolve pelas tools:

```
paguei 1.500 de aluguel hoje          → despesa paga
vendi 3200 no balcão                  → venda (faturamento)
conta de luz de 300 vence dia 20      → despesa pendente
o convênio me deve 2000               → venda a receber
como foi o mês?                       → balanço + pendências
minha meta é 80.000 de faturamento    → define a meta do mês
como está a meta?                     → progresso
```

Cada entrada carrega uma **origem** e o agente a escolhe pela frase: uma venda
é venda, e dinheiro que entrou sem venda — empréstimo, aporte de sócio,
rendimento, restituição — entra com a origem certa e fica fora do faturamento.

Os comandos de barra que existiam aqui foram removidos: a regex que lia o valor
tratava o ponto de milhar como decimal, então `/despesa 1.500 aluguel` gravava
R$ 1,50 em silêncio. Detalhes e o que se perdeu com eles (séries recorrentes)
em [`docs/whatsapp.md`](docs/whatsapp.md).

### Faturamento × Entradas de caixa

São duas métricas distintas, e o dashboard mostra as duas:

- **Faturamento** — só venda de produto ou serviço, contada no dia da venda
  (venda no crediário conta na hora da venda, mesmo sem ter sido paga). Todo
  indicador de desempenho usa esta: metas, projeções, crescimento, comparações.
- **Entradas de caixa** — todo dinheiro que efetivamente entrou, incluindo
  empréstimos e aportes, contado no dia em que chegou. Serve para fluxo de caixa
  e liquidez, nunca para desempenho: empréstimo não é crescimento.

Cada entrada carrega uma **origem** explícita (venda, empréstimo, aporte de
sócio, receita financeira, restituição, recebimento de cliente, outros).
Ver [ADR-016](docs/adr/ADR-016-faturamento-entradas-caixa.md).

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

- Lambda (webhook + dashboard API + notifier)
- API Gateway HTTP (rotas explícitas + `/{proxy+}`)
- DynamoDB (single-table: entries, goals, categories, users, tokens)
- segredos (webhook secret, Gemini key, Meta token) como env vars da Lambda, injetadas via `TF_VAR_*`
- Cloudflare DNS (CNAME apontando pro API Gateway)

O DNS tem `lifecycle.ignore_changes` no content para não ser alterado acidentalmente se o gateway mudar.

**Ship**: o deploy roda no GitHub Actions (botão manual, `workflow_dispatch`) autenticado por OIDC — sem chaves AWS estáticas; PRs recebem um comentário com o `tofu plan`. O state fica remoto no S3. Runbook completo (bootstrap, secrets, migração de state): [`docs/deploy.md`](docs/deploy.md).

```bash
# break-glass local (mesmo state remoto):
make tofu-plan
make tofu-apply
```

## Pacotes

- `packages/domain`: contratos e regras centrais do domínio (entry, origem, meta, datas).
- `packages/finance`: entradas financeiras, metas mensais, summaries, categorias e as tools do agente.
- `packages/finance/analytics`: saúde do mês, tendências, média por dia da semana, projeção e posição de caixa.
- `packages/notifications`: regras de alerta (vence hoje, vencidas, meta batida).
- `packages/orchestrator`: coordena histórico, tools e o provider de IA (Gemini ou Ollama).
- `packages/whatsapp`: cliente da Meta Cloud API (+ cliente local do simulador).
- `packages/wasession`: janela de 24h do WhatsApp e dedupe de mensagens (TTL).
- `packages/conversation`: histórico curto da conversa (TTL).
- `packages/payments`: domínio de adquirentes, parser do PagBank e importer.
- `packages/dynamostore`: seam do DynamoDB + tabela fake usada nos testes (ADR-014).
- `packages/shared`: config, fuso da farmácia e o id do ledger compartilhado.
- `apps/webhook`: handler Lambda e runner local para webhook do WhatsApp.
- `apps/dashboard-api`: API REST do painel financeiro (Lambda + local).
- `apps/notifier`: Lambda agendada — resumo diário e a lista de contas do dia (ADR-023).
- `apps/payment-importer`: Lambda que importa extratos de adquirentes do S3.
- `apps/web`: frontend React + shadcn/ui + Recharts.
- `apps/cli`: fluxo local para exercitar o orchestrator sem WhatsApp.
