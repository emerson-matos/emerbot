# Architecture

## Premissas

- custo abaixo de `R$20/mês`
- arquitetura serverless
- domínio isolado de AWS e de providers de IA
- multiusuário desde a primeira versão

## Decisões

- `apps/webhook` recebe o contrato externo e normaliza para `domain.Message`.
- `packages/orchestrator` controla o fluxo de ponta a ponta.
- `packages/llm` define apenas o contrato do provider.
- `packages/memory` separa curto e longo prazo por interface.
- `packages/tools` centraliza registro e execução controlada.

## Modelo de dados

### Messages

- `PK`: `UserId`
- `SK`: `Timestamp`
- `TTL`: `ExpiresAt`
- uso: contexto recente da conversa

### Memories

- `PK`: `UserId`
- `SK`: `MemoryKey`
- sem TTL
- uso: preferências, hábitos, metas e fatos persistentes

## Evolução sugerida

1. mover persistência in-memory para adapters DynamoDB
2. adicionar fila assíncrona apenas quando houver um padrão de acesso claro
3. manter tools atrás de contracts explícitos
4. manter provider de IA trocável via `llm.Client`
