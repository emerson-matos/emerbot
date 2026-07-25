# ADR-013: Importação de dados de adquirentes (PagBank EDI)

## Status

Accepted

## Contexto

As vendas no cartão eram digitadas à mão (ou não eram digitadas). Sem elas o
fluxo de caixa só enxergava despesas, e a projeção não respondia à pergunta que
importa para a farmácia: *quanto vai entrar, e quando*. Os adquirentes já expõem
esses dados via EDI (PagBank hoje, Stone depois), com dois extratos distintos —
o **transacional** (a venda e suas parcelas previstas) e o **financeiro**
(a liquidação, quando o dinheiro de fato cai).

Restrições: teto de custo (ADR-008), tabela única de finanças (ADR-005) e nada
de serviço rodando 24/7.

## Decisão

- **Ingestão desacoplada do processamento.** `scripts/pagbank-import` monta um
  envelope JSON combinado a partir dos extratos EDI e o envia ao S3; o evento
  `ObjectCreated` dispara a Lambda `payment-importer`. O envelope no S3 é o
  registro do import (versionado), e reprocessar é re-subir o objeto.
- **Um script só, local e remoto.** O mesmo `pagbank-import` importa direto no
  dynamodb-local (`-target dynamodb`) ou pelo S3 (`-target s3://…`). Os dois
  caminhos constroem o mesmo `packages/payments/importer` — por isso ele mora em
  `packages/` e não sob o `internal/` de um app: se o caminho local fosse código
  próprio, "funciona local" deixaria de dizer alguma coisa sobre produção.
  Detalhes de uso em [`docs/payments-import.md`](../payments-import.md).
- **Domínio canônico agnóstico de provider** (`packages/payments`):
  `Sale` / `ExpectedReceivable` / `Payment`. Os parsers são tradutores puros e
  determinísticos; o `ImportService` só orquestra; o `Repository` é dono do
  layout de persistência e da estratégia de replace.
- **Um import = um `(Provider, SourceDate)`.** `ValidateImportResult` recusa,
  antes de qualquer escrita, um resultado que misture providers ou dias de
  origem, ou que contenha dois itens com a mesma chave de armazenamento.
- **Replace idempotente por item de índice.** Cada import grava um item
  `IMPORT#<provider>#<data>` com a lista de SKs que escreveu. A próxima execução
  do mesmo dia lê esse item (um `GetItem`) para saber o que apagar. A
  alternativa — varrer a partição filtrando por `SourceDate` — é cobrada por
  item lido *antes* do filtro, ou seja, releria todo o histórico a cada import,
  com custo crescendo sem limite (incompatível com o ADR-008).
- **Sem atomicidade, com convergência.** A substituição de um dia não cabe numa
  transação, então não se finge que é atômica: as escritas vão em
  `BatchWriteItem` e o item de índice é gravado **por último**. Uma falha no
  meio deixa o índice ainda descrevendo a versão anterior, e a reexecução
  reconstrói o conjunto completo de deleções.
- **Sem DLQ; o log basta.** A invocação por S3 é assíncrona e descarta o evento
  depois de duas retentativas, mas o envelope continua no bucket — nada se
  perde de fato. A Lambda registra `bucket` e `key` de toda falha, e recuperar
  é re-subir o objeto. Uma fila só para guardar um ponteiro para um arquivo que
  já está guardado não se paga nesta fase (ADR-008).

## Consequências

- as vendas no cartão entram sozinhas e alimentam a projeção de caixa
- adicionar a Stone é escrever um parser: nada em `ImportService` muda
- reprocessar um dia é re-subir o objeto — a operação é sempre a mesma

### Invariante: venda no cartão não entra duas vezes

A projeção soma os recebíveis importados **por cima** do `ProjectedIncome` do
razão. Isso pressupõe que a venda no cartão chega ao sistema **pelo import e não
também como `FinancialEntry` de receita**. Uma venda registrada nos dois lugares
é contada duas vezes na projeção, e nada a jusante consegue detectar isso — não
há identificador em comum entre um lançamento digitado no WhatsApp e uma
transação do EDI. Se um dia as vendas de balcão passarem a ser lançadas
manualmente de novo, a projeção precisa de uma regra de deduplicação antes.

### Fora de escopo nesta fase

Cancelamentos e chargebacks (eventos 3, 5, 6, 26, 27…), antecipação como evento
próprio, extrato de `balances`, fetch automático e o adapter da Stone. Só o
evento `1` (venda/pagamento) é importado.
