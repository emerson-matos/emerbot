# WhatsApp (Meta Cloud API)

O webhook recebe mensagens do WhatsApp Business Platform (Meta Cloud API),
registra lançamentos financeiros a partir de comandos e responde ao remetente.
Código em `apps/webhook` (+ `packages/whatsapp`).

## Como funciona

- **Verificação (GET `/webhook`)**: a Meta chama com `hub.mode=subscribe`,
  `hub.verify_token` e `hub.challenge`. O app confere o token contra
  `WEBHOOK_VERIFY_TOKEN` e devolve o `hub.challenge` em texto puro.
- **Mensagens (POST `/webhook`)**: a Meta envia o envelope
  `object → entry[] → changes[] → value.messages[]`. O app:
  1. valida a assinatura **`X-Hub-Signature-256`** (HMAC-SHA256 do corpo cru com
     o **App Secret**) — assinatura inválida ⇒ 401;
  2. `FromWAWebhook` percorre **todas** as entries/changes/messages (um POST pode
     trazer várias mensagens em lote), ignora `statuses[]` (entregue/lido) e
     mensagens que não sejam `type: "text"`;
  3. processa cada mensagem e responde **um único 200**. A Meta **re-tenta
     qualquer resposta ≠ 200 por até 7 dias**, então só devolvemos erro para
     assinatura inválida (401) ou JSON malformado (400).
- **Resposta**: via `packages/whatsapp` (`MetaClient`) usando
  `META_GRAPH_API_TOKEN`; o destino é o telefone do remetente e o
  `phone_number_id` vem do `metadata` do envelope.

## Configuração no Meta

No [Meta App Dashboard](https://developers.facebook.com/apps) → WhatsApp:

1. **Webhook / Callback URL**: `https://webhook.<seu-domínio>/webhook`
   (o custom domain do API Gateway — veja [cloudflare-dns.md](./cloudflare-dns.md)).
2. **Verify token**: um valor arbitrário que você define aqui e em
   `WEBHOOK_VERIFY_TOKEN`.
3. **Assinar o campo `messages`** (Webhook fields → subscribe `messages`).
4. **App Secret** (Configurações → Básico): use como `WEBHOOK_SECRET` — é a chave
   do HMAC que valida os POSTs.
5. **Access token permanente** (System User token com permissão
   `whatsapp_business_messaging`): use como `META_GRAPH_API_TOKEN`.
6. Anote o **Phone number ID** do número de teste/produção.

## Variáveis de ambiente

| Var | Uso |
|-----|-----|
| `WEBHOOK_SECRET` | **App Secret da Meta** — valida o HMAC `X-Hub-Signature-256`. Também é o fallback do verify token. |
| `WEBHOOK_VERIFY_TOKEN` | token do handshake GET (default: `WEBHOOK_SECRET`). |
| `META_GRAPH_API_TOKEN` | token da Graph API para enviar respostas. Vazio ⇒ usa o cliente local (simulador). |
| `FINANCIAL_ENTRIES_TABLE` / `DYNAMODB_ENDPOINT` | store dos lançamentos. |

Em produção essas variáveis são injetadas pelo OpenTofu (`TF_VAR_*`); localmente,
via `.env`.

## Comandos suportados

`/despesa`, `/receita`, `/pagar`, `/receber`, `/resumo`, `/goal`, `/meta` e
**`/help`** (alias `/ajuda`), que lista todos os comandos. O `/help` é a fonte
única da lista — veja `commandHelp` em `apps/webhook/internal/app/app.go`.

## Teste local

O simulador já fala o contrato real da Meta (envelope + assinatura HMAC com
`WEBHOOK_SECRET`):

1. `make up` (ou `make demo`).
2. Abra o simulador em `http://localhost:9000` e envie `/help` ou
   `/despesa 500 aluguel`.
3. A resposta do bot aparece no simulador (o webhook a entrega via `/reply`).

## Debug

- Logs do Lambda em produção: **CloudWatch** (grupo
  `/aws/lambda/emerbot-dev-webhook`).
- Mensagens ignoradas (status/tipo não-texto) e falhas de assinatura são logadas.
- 401 constante ⇒ `WEBHOOK_SECRET` não bate com o App Secret da Meta.
