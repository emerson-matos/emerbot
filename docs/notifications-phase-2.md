# Notificações — Fase 2 (alertas por WhatsApp)

Este documento descreve os próximos passos para a feature de notificações. A
**Fase 1 já está implementada**; a **Fase 2** exige backend e ainda não foi
feita.

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
