# ADR-015: Faturamento e entradas de caixa são métricas distintas

## Status

Accepted

## Contexto

O sistema usava "receita" para qualquer entrada de dinheiro. Isso mistura dois
conceitos que gestores e contadores tratam separadamente, e a consequência
prática é que um empréstimo aparecia como crescimento do negócio.

Já tentamos resolver isso duas vezes e oscilamos:

- `636dba8` unificou "Faturamento" e "Receita" numa figura só, na teoria de que
  toda categoria de entrada era uma venda.
- `eea332a` padronizou os identificadores em `Income`/`Receita`.
- `09ce297` reintroduziu faturamento como **exclusão de categoria**:
  `Category != "outros_receitas"`.

A terceira versão funcionava por acidente. A regra era frágil em dois pontos:

1. **Qualquer categoria de entrada criada pelo usuário contava como
   faturamento.** Uma categoria "Empréstimos" feita à mão entrava na meta.
2. **Um empréstimo só ficava de fora se a pessoa lembrasse de arquivá-lo em
   "Outros (Receita)".** Ou seja, a correção do número dependia de o usuário
   escolher a categoria certa por um motivo que a UI nunca explicava.

E o predicado vivia triplicado — `finance.IsFaturamento`,
`analytics.isFaturamento` (cópia privada, porque `analytics` não podia importar
os helpers de `finance` de volta sem ciclo) e um terceiro filtro inline em
`apps/web/src/lib/notifications.ts` — mantidos em sincronia à mão.

## Decisão

A origem do dinheiro passa a ser um **campo explícito do lançamento**
(`domain.IncomeOrigin`), e as duas métricas derivam dele:

- **Faturamento** = entradas com origem `venda`. É o que a farmácia vendeu.
- **Entradas de caixa** = todo dinheiro efetivamente recebido, qualquer origem.

Origens: `venda`, `recebimento_cliente`, `emprestimo`, `aporte_socio`,
`receita_financeira`, `restituicao`, `outros`.

**Todo indicador de desempenho lê faturamento** — metas, projeções, comparações
entre períodos, crescimento, médias por dia da semana, ritmo semanal,
recomendações e os insights da IA. Entradas de caixa servem exclusivamente para
fluxo de caixa e liquidez.

O predicado é `domain.IsRevenue`, uma função só, em `domain` (junto da
taxonomia) para que `analytics` a alcance sem o ciclo de import que obrigava as
duas cópias.

### O campo não se chama `Source`

`domain.EntrySource` já existe e significa o *canal* que criou o registro
(WhatsApp, dashboard). O campo novo é `Origin`.

### Três bases de data, deliberadamente

Esta é a parte que mais surpreende, e é onde estava o erro silencioso: as três
perguntas se referem a três dias diferentes.

| Métrica | Data | Por quê |
|---|---|---|
| Faturamento | `TransactionDate` | Uma venda conta quando é feita. Venda no crediário é faturamento no dia da venda, paga ou não. |
| Entradas de caixa | `PaymentDate` | Dinheiro conta quando chega. Pendente não é caixa. |
| Previsto (`TotalExpectedIn`/`TotalExpense`) | `EffectiveDate` | "Quanto este mês deve movimentar", contas a vencer incluídas. Comportamento histórico do ledger. |

Uma venda feita em 5/jan com vencimento em 5/fev, paga em 11/mar, é: faturamento
de **janeiro**, previsto de **fevereiro**, entrada de caixa de **março**. Nenhuma
das três respostas é redundante e nenhuma pode ser derivada das outras
filtrando em memória — é por isso que `MonthlySummary` carrega os três totais e
`monthlySummary` faz uma query por base.

Antes desta mudança as bases já divergiam **sem intenção**: os sumários
agrupavam por `EffectiveDate` enquanto o `analytics` calculava faturamento por
`TransactionDate`. O KPI e a meta discordavam sobre em que mês uma venda a prazo
caía, e nada apontava isso.

### O GSI1 vira o índice de caixa

Consultar por `PaymentDate` precisa de um índice. O `GSI1-Category`
(`GSI1SK = "<categoria>#<data>"`) era escrito em todo lançamento e **nunca
consultado** — nenhuma `Query` no Go usava esse índice, e filtro por categoria
sempre saiu por filter expression na GSI2. Ele consumia 8 RCU / 8 WCU dos 25 do
free tier.

Passa a indexar `"<paymentDate>#<entryID>"`, **esparso**: um lançamento pendente
omite `GSI1PK`/`GSI1SK` e simplesmente não está no índice, o que é a semântica
correta — dinheiro que não se moveu não é caixa. Custo: zero índice novo, zero
mudança de capacidade, zero diff de Terraform (os atributos já eram declarados).

O nome físico continua `GSI1-Category`, com comentário na constante
`gsi1IndexName`: renomear obrigaria o DynamoDB a destruir e reconstruir o
índice, e o repo já tem precedente de nome de wire legado (o atributo
`RevenueTarget` do `goalItem`).

Faturamento não precisou de índice: o SK da tabela base já é
`ENTRY#<transactionDate>#<id>`.

### Vocabulário

`Revenue` (faturamento) e `CashIn` (entradas de caixa) nos identificadores Go e
JSON; "Faturamento" e "Entradas de Caixa" na UI, no WhatsApp e nos prompts.

"Receita" **desaparece como rótulo** — era a palavra ambígua que originou o
problema. `EntryTypeIncome` fica: é o *tipo* do lançamento (entrada vs saída),
não a métrica. `Goal.RevenueTarget` volta a coincidir com o nome do atributo já
gravado no DynamoDB.

### O shim de migração

Lançamentos escritos antes do campo não têm origem. `domain.IsRevenue` trata
`Origin == ""` caindo de volta na regra antiga por categoria, para que
faturamento tenha **exatamente** o mesmo valor de antes nesses lançamentos.
`scripts/migrate-origin` faz o backfill com o mesmo mapeamento, então a migração
não move nenhum número — a melhora vem dos lançamentos novos, já etiquetados.

`Normalize()` **não** preenche origem vazia. Preencher com `venda` faria todo
`outros_receitas` não migrado voltar a *entrar* no faturamento (`itemToEntry`
normaliza antes de validar, em toda leitura), invertendo o comportamento atual —
exatamente o bug que este trabalho existe para matar.

O shim é o único ponto irreversível, e é uma função só. Apagar depois que a
migração rodar em todo ambiente, junto com o `Origin === ''` em
`apps/web/src/lib/notifications.ts` e o teste que o cobre.

### Ordem de deploy

1. Deployar **todas** as Lambdas (webhook, dashboard-api, notifier,
   payment-importer). Enquanto uma antiga continuar escrevendo
   `GSI1SK = "<categoria>#<data>"`, o índice fica misto — e um range
   `BETWEEN "2026-01-01" AND "2026-01-31#\xff"` não casa nada dos escritores
   velhos, porque slug minúsculo ordena acima de data com dígito. É erro por
   omissão silenciosa, não exceção.
2. Rodar `scripts/migrate-origin --dry-run`, depois sem a flag. Conferir que os
   totais do mês não mudaram.
3. Apagar o shim.

O passo 1 é seguro precisamente porque nada consulta o GSI1 hoje.

## Consequências

**Some a limitação documentada em `analytics/goals.go`.** A tabela de 3 meses
lia o total amplo contra uma meta de vendas: um mês passado com um empréstimo
parecia mais perto do alvo do que estava. Agora `MonthlySnapshot.Revenue` e
`RevenueTarget` medem a mesma coisa.

**Somem quatro round-trips.** `tools.go`, `webhook/handler.go`, `notifier.go` e
`analytics/service.go` reliam os lançamentos crus do mês só para calcular
faturamento, porque o sumário não sabia separar. Agora leem o campo.

**`monthlySummary` custa três queries em vez de uma.** É o preço de as três
perguntas existirem. Numa tabela deste tamanho é irrelevante em RCU, e a
alternativa — ler uma faixa e reagrupar em memória — perde silenciosamente a
venda a prazo e o recebimento atrasado.

**`Analysis` ganha `schemaVersion`.** O notifier persiste o `Analysis` como
snapshot diário e o dashboard-api desserializa o de *ontem* no struct de *hoje*
para diffar. `encoding/json` deixa um campo renomeado no valor zero, então sem
versão o dia do deploy reportaria "faturamento foi de R$ 0,00 para R$ 45.000".
Consumidores recusam comparar entre versões em vez de adivinhar.

**`EntryFilter.Cursor` e `Limit` só valem na base `EffectiveDate`.** Os dois
significam "os N mais recentes por data efetiva", que nenhuma outra base ordena.
`EntryFilter.Validate` rejeita a combinação: paginar contra a chave de ordenação
errada descartaria lançamentos de uma tela financeira sem erro nenhum a mostrar.

**`recebimento_cliente` fica fora do enum da tool de criação da IA.** Um cliente
quitando um crediário é o lançamento pendente *existente* sendo marcado como
pago — a venda já foi registrada no dia em que aconteceu. Oferecer ao modelo uma
origem que soa como "cliente me pagou" faz ele criar um segundo lançamento e
contar a venda duas vezes, sem nada a jusante capaz de detectar. A origem
continua alcançável no formulário web, onde significa "recebível anterior a este
ledger".

**O fake de DynamoDB passa a rejeitar string vazia em atributo-chave.** Sem
`omitempty` nas tags do GSI1 o marshaller escreve `""`, que o DynamoDB real
recusa — todo `/pagar` e `/receber` daria 500 em produção. O fake só checava
presença, então aceitava e até devolvia o item em queries de índice: suíte verde,
produção quebrada. Isso é o contrato do ADR-014 ("erra no que não modela") sendo
cumprido.
