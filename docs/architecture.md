# Architecture

## Premissas

- custo abaixo de `R$20/mês`
- arquitetura serverless
- domínio isolado de AWS e de providers de IA
- **um ledger, duas pessoas**: a farmácia tem uma contabilidade só. Pai e filho
  entram com contas próprias no Cognito, e as duas leem e escrevem o mesmo
  ledger (`shared.FinanceLedgerID`). Isso não é um mock a caminho de
  multiusuário — é o produto. O que é por pessoa é só o endereço para onde as
  mensagens do WhatsApp vão (`domain.NotificationPrefs`, chaveado pelo `sub` real
  do Cognito).

## Decisões

- `apps/webhook` recebe o contrato externo e normaliza para `domain.Message`.
  Não há camada de comandos: toda mensagem vai para o agente (ver
  [whatsapp.md](./whatsapp.md)).
- `packages/orchestrator` controla o fluxo de ponta a ponta e é o único lugar
  que escolhe o provider de IA (`NewTextGenerator`).
- `packages/finance` expõe as tools que o agente chama; os argumentos chegam
  tipados, nunca como texto a ser interpretado.
- `packages/finance/analytics` é dono de todo número derivado — projeção, meta
  do dia, posição de caixa. Nenhum consumidor (dashboard, notifier, agente)
  recalcula: eles citam. Ver ADR-019 a ADR-022.

## Modelo de dados

Três tabelas DynamoDB, todas `PROVISIONED` para caber no free tier de 25/25
(ADR-005 explica por que não é tabela única).

### `financial-entries`

- `PK`: `USER#<ledger>` · `SK`: o tipo de item (entry, goal, categoria,
  destinatário de notificação, snapshot do dia)
- `GSI1` — índice por data de caixa (data de pagamento)
- `GSI2` — índice por data efetiva (`DueDate`, senão `TransactionDate`); é o que
  todo `ListEntries` usa
- sem TTL: é o livro-caixa

### `conversations`

- `PK`: telefone · `SK`: cronológico
- `TTL`: `ExpiresAt`
- uso: contexto recente da conversa para o agente

### `whatsapp-sessions`

- `PK`: `Phone`
- `TTL`: `ExpiresAt` (20h)
- uso: janela de atendimento de 24h da Meta + dedupe de `message_id`

## Evolução sugerida

1. proteger o webhook com uma allowlist dos dois telefones — hoje qualquer
   número que escreva para a linha entra no ledger
2. ligar PITR e deletion protection na `financial-entries`
3. manter tools atrás de contracts explícitos
4. manter provider de IA trocável via `orchestrator.TextGenerator`
