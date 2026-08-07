# WhatsApp (Meta Cloud API)

O webhook recebe mensagens do WhatsApp Business Platform (Meta Cloud API),
entrega cada uma ao agente — que registra os lançamentos pelas tools do
`packages/finance` — e responde ao remetente. Código em `apps/webhook`
(+ `packages/whatsapp`).

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

## Não há comandos

Toda mensagem — inclusive uma que comece com barra — vai para o agente. Escreva
em português normal: *"paguei 1.500 de aluguel"*, *"vendi 1200 no balcão"*,
*"quanto ainda tenho a pagar esse mês?"*. Quem escreve no ledger são as tools
(`packages/finance.FinanceTools`: criar, editar, resumo do mês, definir meta,
listar contas a vencer, buscar lançamentos) mais o `get_analysis`
(`packages/finance/analytics`).

Existiu um conjunto de comandos (`/despesa`, `/receita`, `/pagar`, `/receber`,
`/recorrente`, `/resumo`, `/goal`, `/meta`, `/help`) que lia o valor do texto com
regex. Ele foi removido, não corrigido: a regex lia o ponto de milhar do pt-BR
como separador decimal, então `/despesa 1.500 aluguel` gravava **R$ 1,50** numa
categoria chamada `"0"` — sem erro nenhum — e `/meta 80.000,00` definia uma meta
de **R$ 8.000.000,00**. As tools recebem o valor já como número, então não sobra
texto para interpretar errado.

**O que se perdeu:** criar uma série recorrente (o antigo `/recorrente`, N
parcelas de uma vez). Não há tool equivalente nem formulário no dashboard —
`SaveEntries` continua no store, só não tem quem a chame. Hoje é lançamento a
lançamento.

## Teste local

O simulador já fala o contrato real da Meta (envelope + assinatura HMAC com
`WEBHOOK_SECRET`):

1. `make up` (ou `make demo`).
2. Abra o simulador em `http://localhost:9000` e mande uma frase — os atalhos no
   topo já trazem exemplos.
3. A resposta do bot aparece no simulador (o webhook a entrega via `/reply`).

> Sem `GEMINI_API_KEY` (ou com `LLM_PROVIDER=ollama` e o Ollama no ar, ADR-012) o
> orchestrator cai no `StaticClient`, que só ecoa o texto: nada é gravado. Com os
> comandos, alguns fluxos funcionavam sem LLM; agora o agente é o único caminho.

## Debug

- Logs do Lambda em produção: **CloudWatch** (grupo
  `/aws/lambda/emerbot-dev-webhook`).
- Mensagens ignoradas (status/tipo não-texto) e falhas de assinatura são logadas.
- 401 constante ⇒ `WEBHOOK_SECRET` não bate com o App Secret da Meta.
