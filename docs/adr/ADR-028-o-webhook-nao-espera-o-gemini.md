# ADR-028: O webhook não espera o Gemini

## Status

Accepted

## Contexto

O webhook responde à Meta só depois de o agente ter terminado. Isso amarra o
contrato de entrega da Meta ao tempo do Gemini, e o resultado apareceu em
produção como silêncio: `oi` era respondido e `como estamos` não.

O diagnóstico (PR #98) encontrou três coisas somadas. Duas eram bugs e foram
corrigidas; a terceira é estrutural e é o motivo deste ADR:

- o agente orça 25s para si, em até 5 rodadas de ferramenta, dentro de uma
  Lambda que dava 10 — a plataforma matava o processo antes de o orçamento do
  agente valer;
- falhar era ficar mudo, porque o handler devolvia 500 sem enviar nada;
- **uma pergunta que chama ferramenta leva dezenas de segundos, e a Meta está
  esperando por elas.**

O teto de 30s do PR #98 é uma aposta de que o Gemini permaneça rápido. Uma
rodada a mais de ferramenta, ou um dia ruim da API, e o sintoma volta idêntico.
Enquanto o agente estiver no caminho síncrono, a resposta ao usuário e o ACK
para a Meta são o mesmo evento — e não deveriam ser.

O ADR-008 (teto de R$20/mês) restringe a escolha do mecanismo. O custo real hoje
é ~R$0,03/mês, com dois usuários e uma farmácia.

## Decisão

Duas Lambdas, com uma fila SQS FIFO entre elas.

```
Meta ──POST──► API Gateway ──► webhook Lambda
                                 1. valida X-Hub-Signature-256
                                 2. extrai a mensagem
                                 3. SendMessage
                                 4. 200 OK          (~50ms)
                                         │
                                         ▼
                                    SQS FIFO
                             MessageGroupId = telefone
                                         │
                                         ▼
                                  worker Lambda
                                 histórico · Gemini · tools
                                 · resposta no WhatsApp
                                         │
                                  esgotou as tentativas
                                         ▼
                                        DLQ
```

**O webhook não sabe o que é Gemini.** Não conhece histórico, tools, o DynamoDB
de negócio nem a resposta do agente. O contrato dele passa a ser uma frase:
recebi e enfileirei.

**FIFO com `MessageGroupId` = telefone.** Esta é a razão de ser uma fila e não
uma invocação assíncrona de Lambda. O agente lê o histórico da conversa como
entrada, então duas mensagens do mesmo telefone processadas em paralelo não são
duas execuções independentes — são uma execução lendo um passado que ainda não
aconteceu:

```
A: "anota 40 reais de fiado do joão"
B: "quanto o joão me deve?"
```

Sem serialização, B pode ler o histórico antes de A ter escrito, e responder com
confiança um número errado. Isso é propriedade do domínio, não otimização. O
grupo por telefone dá ordem *dentro* de uma conversa e mantém conversas
diferentes em paralelo — que é exatamente o paralelismo que um bot de conversa
quer.

**A entrega é responsabilidade da infraestrutura.** Retry com backoff e DLQ vêm
da fila. O `defer` compensatório que devolvia o marcador de dedup quando o turno
falhava deixa de existir nessa forma (ver ADR-029).

**O fallback sai do worker, não de uma Lambda na DLQ.** O SQS entrega
`ApproximateReceiveCount` em cada mensagem, então o worker sabe quando está na
última tentativa e avisa o usuário ali. Pôr um ESM na DLQ significaria uma
segunda fila sondada continuamente pelo mesmo trabalho, e a mensagem chegaria
depois de a infraestrutura ter desistido — mais tarde e mais cara.

**Enfileirar a mensagem extraída, não o envelope da Meta.** O tarifador do SQS
cobra cada 64 KB de payload como uma requisição, então tamanho é preço; e o
worker não deveria reparsear o formato da Meta para saber o que fazer.

### Por que SQS e não invocação assíncrona de Lambda

A invocação assíncrona não tem polling e o custo é estritamente proporcional ao
uso. Perde ordenação e dedup de transporte — e reconstruir ordenação por
conversa em cima do DynamoDB é escrever um mecanismo de fila à mão, que é
precisamente o tipo de coisa cujo bug este projeto acabou de pagar.

O custo do SQS foi medido contra o teto, não estimado:

- a franquia é de **1 milhão de requisições/mês, perpétua** (não 12 meses) e
  **igual para FIFO e Standard** — a ordenação não é cobrada a mais;
- **não há tarifa mínima**: o SQS não tem mensalidade, o custo é por requisição;
- o polling do event source mapping gera `ReceiveMessage` mesmo com a fila
  vazia, e a AWS é explícita em que essas chamadas são faturáveis "including
  those that return empty results", sem desconto para long polling;
- **a franquia é da conta inteira**, somando todas as filas — é por isso que a
  DLQ não ganha um poller.

**Não assumimos um número de `ReceiveMessage`/mês.** As estimativas plausíveis
para uma fila ociosa variam por um fator de cinco conforme quantos pollers o
ESM mantém em repouso, que a AWS não documenta no modo não-provisionado. Ambas
cabiam na franquia, então a decisão não dependia de acertar o número — mas a
regra abaixo existe porque não acertamos.

### A guarda de custo

A arquitetura é adotada **enquanto o custo mensal total permanecer abaixo de
R$20**. Se aproximar, revisar o event source mapping (concorrência máxima e
pollers) antes de trocar o mecanismo.

Essa regra vem com gatilho: um **AWS Budget com alerta** (os dois primeiros são
gratuitos). Uma regra que depende de alguém lembrar de olhar o Cost Explorer não
é uma guarda, é uma intenção.

## Consequências

- a Meta recebe 200 em ~50ms e para de esperar pelo Gemini; o teto de tempo do
  worker deixa de ser uma aposta sobre a latência de terceiros
- mensagens da mesma conversa não se atropelam; conversas diferentes seguem em
  paralelo
- falhas ficam visíveis (DLQ) em vez de sumirem, e o usuário é avisado na última
  tentativa em vez de nunca
- o webhook fica pequeno o bastante para ser óbvio: assinatura, extração, envio
- superfície de Tofu nova: fila, DLQ, event source mapping, políticas das duas
  roles, redrive policy, e **`sqs:*` na deploy role** — sem isso o apply do CI
  falha no primeiro recurso (a role e o recurso vão na mesma PR, por convenção
  do repositório)
- um quinto zip de Lambda no Makefile, e `apps/worker` passa de placeholder a
  `cmd/lambda` como as demais
- o visibility timeout da fila tem de ser **≥ o timeout da função** — a AWS
  valida isso e recusa o ESM; a recomendação é 6× (worker de 60s → 360s)
- batch do FIFO é no máximo 10 mensagens (Standard vai a 10.000), irrelevante
  neste volume
- entrega é **at-least-once**: a fila reduz duplicatas, não as elimina, e o
  processamento precisa ser idempotente por conta própria (ADR-029)
- o PR #98 não vira trabalho perdido: ele é a rede que segura o sistema até esta
  arquitetura existir, e o fallback continua sendo o que impede o silêncio
