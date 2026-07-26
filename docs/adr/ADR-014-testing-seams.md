# ADR-014: Costuras de teste — DynamoDB falso e conformidade entre implementações

## Status

Accepted

## Contexto

Todo store de DynamoDB guardava um `*dynamodb.Client` concreto. Não havia como
exercitar nenhum deles sem uma tabela real, então ~1500 linhas de código de
persistência ficavam em 0% de cobertura: o layout de chaves, as condições de
range no GSI, as expressões de filtro e as escritas condicionais nunca eram
verificadas. Os quatro stores também repetiam o mesmo bloco de
`LoadDefaultConfig`/`BaseEndpoint`.

Pior que a cobertura: `finance.Store` tem duas implementações (DynamoDB e em
memória) e o caller escolhe por configuração. Sem nada comparando as duas, elas
divergiram em silêncio — `MonthlySummary("julho")` devolvia erro no DynamoDB e
um sumário zerado em memória. Um mês digitado errado renderizava R$ 0,00 como se
fosse dado real no stack local e 500 em produção.

Alternativas consideradas:

- **`dynamodb-local` nos testes unitários.** Pega o comportamento real, mas
  exige container no CI, deixa a suíte lenta e torna cada teste dependente de
  estado externo. Continua sendo o certo para o `make demo`, não para `go test`.
- **Gravadores de chamada (call recorders).** Fáceis, mas só afirmam "o store
  chamou PutItem com esses argumentos". Não dizem se a query devolve as linhas
  certas, e não pegam nada de ordenação, paginação ou condição.

## Decisão

- **`packages/dynamostore`** expõe a interface `API` com as sete operações que o
  repo de fato emite, mais um `NewClient` único. Cada store depende da interface
  e ganha um construtor `…WithClient` para injeção.

- **`packages/dynamostore/dynamotest`** é uma tabela DynamoDB em memória que
  implementa essa interface de verdade: chaves compostas, GSI esparso, ordenação
  e `ScanIndexForward`, `Limit` aplicado **antes** do filtro, paginação com
  `LastEvaluatedKey`, escritas condicionais, `UnprocessedItems` e a atomicidade
  de `TransactWriteItems`. Inclui um parser das expressões que o repo escreve
  (comparações, `BETWEEN`, `IN`, as quatro funções de atributo, `AND`/`OR`/`NOT`
  com precedência e parênteses).

  A propriedade que dá valor ao fake: **o que ele não modela vira erro
  explícito**, nunca condição silenciosamente verdadeira. Uma função
  desconhecida, uma key condition tocando atributo que não é de chave, uma
  escrita sem a chave — tudo isso quebra o teste, como o DynamoDB real
  quebraria a requisição. Um fake que ignorasse a expressão que não entende
  daria luz verde a uma query impossível. O fake tem os próprios testes por
  isso.

- **Suíte de conformidade** (`packages/finance/store_conformance_test.go`) roda
  o mesmo cenário contra as duas implementações de `Store`. Divergência entre
  elas passa a ser teste vermelho, não bug que só aparece num ambiente.

- **Regra derivada:** comportamento compartilhado por várias implementações de
  uma interface é escrito **uma vez**. Os três sumários (`MonthlySummary`,
  `CategorySummary`, `CashFlowForecast`) são views derivadas de `ListEntries` e
  moram em `packages/finance/summaries.go`; as duas implementações delegam. Foi
  a duplicação deles que produziu a divergência acima.

- **Interfaces por consumidor.** `finance.Store` tem 17 métodos; o notifier usa
  5, o webhook 6, o handler de payments do dashboard usa 1. Cada consumidor
  declara a interface que consome, no seu próprio pacote (idioma Go). Isso
  isola cada um dos métodos adicionados para os outros, e três dos cinco
  handlers do dashboard deixaram de importar `packages/finance` por completo.

## Consequências

- Os stores de DynamoDB passaram de 0% para 80–90% de cobertura, pelo caminho
  real de requisição, sem container e em milissegundos.
- Adicionar uma operação DynamoDB nova exige estendê-la em `dynamotest`. É
  atrito de propósito: sem isso o fake volta a divergir do serviço.
- O fake não é o DynamoDB. Não modela throughput, tamanho de item, consistência
  eventual em GSI nem custo. `dynamodb-local` (via `make demo`) continua sendo
  o teste de integração; a suíte unitária cobre a lógica.
- Uma terceira implementação de `Store` herda os sumários e é obrigada a passar
  pela conformidade.
