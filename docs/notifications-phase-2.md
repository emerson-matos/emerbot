# Notificações — Fase 2 (alertas por WhatsApp)

**Status: implementada.** As Fases 1 e 2 estão no código. O que resta é
operacional (configurar o `WHATSAPP_PHONE_NUMBER_ID`, aplicar o OpenTofu) e um
follow-up opcional (expor o log de entrega como histórico real na UI).

## Como está montado

| Camada | Onde |
|--------|------|
| Preferências (persistência) | `NotificationPrefs` em `packages/domain`; `Save/Get/ListNotificationPrefs` na `finance.Store` (item `SK=NOTIFPREFS`), chaveado pelo `sub` real do Cognito (`claims.Subject`, nunca sobrescrito pelo mock do ledger compartilhado — ver `GatewayMiddleware`). Cada usuário Cognito tem suas próprias preferências, todas lendo o mesmo ledger financeiro compartilhado. |
| Telefone | Vem do atributo `phone_number` do usuário no Cognito (claim do JWT), nunca digitado no formulário — `apps/dashboard-api/internal/finance/notifications.go` normaliza para dígitos E.164 ao salvar/ler. |
| API | `GET`/`PUT /notifications/preferences` em `apps/dashboard-api/internal/finance/notifications.go`. |
| Regras de alerta | `packages/notifications` — função pura `Evaluate`, gêmea Go do hook `useNotifications` do web (uma fonte de verdade). |
| Job agendado | `apps/notifier` (Lambda) — avalia cada usuário e envia **um resumo diário** por WhatsApp, deduplicado por dia (log `SK=NOTIFLOG#<data>`). |
| Janela de 24h | `packages/wasession` — tabela DynamoDB própria (`whatsapp-sessions`, on-demand, **TTL de 20h**). O webhook grava uma sessão por telefone a cada inbound; o notifier só envia se a sessão estiver ativa. |
| Envio | `whatsapp.Client.SendText` (mensagem proativa, sem `context` de resposta). |
| Frontend | form real em `apps/web/src/pages/Notificacoes.tsx` (`useNotificationPrefs` / `useSaveNotificationPrefsMutation`). |
| Infra | notifier Lambda + IAM + `aws_cloudwatch_event_rule` (EventBridge) no módulo `api_gateway_lambda`; zip novo no `Makefile`. |

### Janela de atendimento de 24h (evita cobrança)

O WhatsApp só permite mensagens **livres** (não-template) dentro de 24h desde a
última mensagem que o usuário enviou ao número. Fora dessa janela só valem
_templates_ aprovados, **cobrados por conversa**. O notifier **nunca** usa
template — logo, no pior caso, um envio fora da janela é apenas **rejeitado**
pela Meta (não gera cobrança). Ainda assim, para não fazer envios inúteis, o
notifier só envia quando há uma sessão ativa (`Result.OutsideWindow` conta os
pulados).

Mecânica (`packages/wasession`), na tabela dedicada `whatsapp-sessions`:

- O webhook grava, a cada inbound, um item `{Phone, ExpiresAt = agora + 20h}`.
- `ExpiresAt` é o atributo de **TTL** do DynamoDB, então o item some sozinho —
  a tabela não cresce.
- O notifier checa `Active(phone, agora)`: item presente **e** `ExpiresAt` no
  futuro. A checagem no read protege contra o atraso de remoção do TTL (o
  DynamoDB pode demorar horas para apagar), então nunca confiamos só na
  presença do item.
- As **20h** (abaixo das 24h reais) dão margem para o horário do job diário e
  desvio de relógio.

Na prática, o usuário manda qualquer mensagem (ex.: `/resumo`) para reabrir a
janela e voltar a receber os alertas. Templates pagos ficam como decisão futura.

### Configuração operacional pendente

- **`WHATSAPP_PHONE_NUMBER_ID`** (Phone number ID do Meta) e
  **`META_GRAPH_API_TOKEN`** precisam estar setados (via `TF_VAR_*`) para o
  notifier enviar. Sem o token, o cliente cai no simulador local.
- **Agenda**: `var.notifier_schedule` (default `cron(0 11 * * ? *)` = 08h em
  São Paulo). Ajuste o fuso do "vence hoje" com `NOTIFIER_TIMEZONE`.
- Rodar `make build-lambdas && make tofu-apply` (constrói também `notifier.zip`).

## Histórico de referência (plano original)

O texto abaixo é o plano que guiou a implementação, mantido para contexto.

## Fase 1 — alertas derivados no cliente (pronto)

Sem backend novo. O hook `useNotifications` (`apps/web/src/lib/notifications.ts`)
deriva os alertas a partir de dados que o painel já busca e cacheia:

- **Vence hoje** — despesa pendente com _effectiveDate_ = hoje.
- **Vencida** — despesa pendente com _effectiveDate_ < hoje (janela de
  `OVERDUE_LOOKBACK_MONTHS` meses, no máximo `MAX_OVERDUE` itens).
- **Meta atingida** — `TotalIncome` do mês ≥ `RevenueTarget` da meta.

Superfícies: o sino no `Header` (`NotificationBell`, com bolinha vermelha quando
há alertas) e a página **Notificações** (`/notificacoes`), que mostra o
_Histórico de Alertas_ e um card **"Alertas por WhatsApp — em breve"** (o
placeholder desta Fase 2).

> Como é derivado do cache, o "histórico" reflete o estado atual, não um log
> persistido. Um log real de entregas nasce junto com a Fase 2 (ver abaixo).

## Fase 2 — o que falta

Objetivo: entregar os mesmos alertas de forma **proativa** no WhatsApp do
usuário, respeitando preferências que ele configura no painel.

### 1. Preferências (persistência)

Item novo no DynamoDB (reaproveitar a tabela de finanças ou a de perfil — ver
[ADR-005](./adr/ADR-005-two-tables.md)):

```
PK = USER#<userID>   SK = NOTIF_PREFS
{ waEnabled: bool, phone: string,
  notifyDueToday: bool, notifyOverdue: bool, notifyGoal: bool }
```

### 2. API

Dois endpoints novos no `dashboard-api`:

- `GET  /notifications/preferences` → devolve o item acima (defaults quando
  ausente).
- `PUT  /notifications/preferences` → valida e grava. Normalizar o telefone para
  E.164 antes de salvar.

Handler em `apps/dashboard-api/internal/finance` (ou um pacote `notifications`),
seguindo o padrão de `goals`.

### 3. Job agendado (avaliação + envio)

Uma Lambda nova disparada por **EventBridge Scheduler** (ex.: 1×/dia de manhã,
`cron` em horário de Brasília):

1. Percorre usuários com `waEnabled = true`.
2. Reusa a mesma lógica de derivação da Fase 1 (portar para Go em
   `packages/finance` ou um `packages/notifications` compartilhado, para o web e
   o job não divergirem).
3. Filtra pelos flags (`notifyDueToday` / `notifyOverdue` / `notifyGoal`).
4. Envia via `packages/whatsapp` (`MetaClient`, `META_GRAPH_API_TOKEN`) — ver
   [whatsapp.md](./whatsapp.md).
5. Grava um log de entrega (`SK = NOTIF_LOG#<ts>`) para **deduplicar** (não
   reenviar o mesmo alerta no mesmo dia) e para virar o histórico real na UI.

Provisionar a Lambda + o schedule no OpenTofu
(`infra/modules/api_gateway_lambda` como referência; adicionar o gatilho do
EventBridge).

### 4. Frontend

Trocar o card placeholder em `apps/web/src/pages/Notificacoes.tsx` pelo form real
de preferências (toggle "Ativar alertas", telefone, checkboxes) — o mock em
`Dashboard.dc.html` já tem o layout. Adicionar os hooks/queries
(`useNotificationPrefs`, `useSaveNotificationPrefsMutation`) espelhando `useGoal`
/ `useSaveGoalMutation`. Quando houver log persistido, apontar o _Histórico de
Alertas_ para ele.

## Custo (cost cap ~R$20/mês)

- Uma execução diária por usuário é desprezível em Lambda/DynamoDB.
- O custo relevante é a **Meta Cloud API**: mensagens iniciadas pelo negócio são
  cobradas por conversa. Manter frequência baixa (agregar num único resumo
  diário em vez de uma mensagem por alerta) e respeitar o opt-in via
  `waEnabled`.
