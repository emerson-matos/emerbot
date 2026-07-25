# Importando dados de adquirentes (PagBank)

Como as vendas no cartão entram no emerbot. Decisões de arquitetura: [ADR-013](adr/ADR-013-payment-imports.md).

## O caminho

```
extratos EDI (JSON)  →  pagbank-import  →  envelope combinado  →  importer  →  DynamoDB
                                              ↓ (deployed)
                                        S3 imports/  →  evento  →  Lambda
```

`scripts/pagbank-import` é o **único** jeito de um envelope entrar no sistema, local
ou em produção. O `-target` muda só o transporte:

| `-target` | O que faz |
|---|---|
| `dynamodb` | roda o importer no próprio processo, contra `DYNAMODB_ENDPOINT` |
| `s3://bucket` | sobe o envelope; o evento `ObjectCreated` roda a Lambda, que importa |
| `-` | imprime o envelope no stdout, sem enviar a lugar nenhum |

Os dois primeiros chamam o mesmo `packages/payments/importer` — mesmo parser,
mesma validação, mesmo repositório, mesma semântica de replace. Nada é
reimplementado para uso local, então um bug não consegue se esconder de um lado só.

## Uso local (cenários gravados)

`make demo` já faz isso. Manualmente:

```sh
make seed-payments
```

Importa cada pasta de `cenarios/` no dynamodb-local. Duas opções fazem esse
comando funcionar:

- **`-rebase YYYY-MM`** desloca todas as datas em blocos de meses inteiros para
  que a mais recente caia no mês pedido. Os cenários gravados são de 2024, e a
  página Adquirentes abre no mês corrente — sem isso ela apareceria vazia. O
  deslocamento é por mês (não por dias) porque as parcelas do PagBank são
  mensais: uma venda no dia 30 liquida no dia 30 dos meses seguintes.
- **`-date YYYY-MM-DD`** define o dia de origem do import, que é a unidade que o
  repositório substitui. O seed dá um dia distinto a cada cenário porque dois
  deles compartilham a mesma data de negócio mais recente — sem isso, um
  substituiria o outro em silêncio.

## Uso real (dados do PagBank)

Baixe os extratos EDI do dia — pelo menos o **transacional** e o **financeiro** —
numa pasta e rode:

```sh
make import-pagbank DIR=~/extratos/2026-07-23
```

Os arquivos são reconhecidos pelo nome: precisa conter `transactional`,
`financial` ou `cashout` (é como o próprio PagBank nomeia os cenários de teste).
Vários arquivos do mesmo tipo são concatenados, então paginação em vários
arquivos funciona. Arquivos não reconhecidos são ignorados com um aviso.

O dia de origem, se não for passado com `-date`, é a data de negócio mais recente
encontrada — determinístico, então montar a mesma pasta duas vezes dá o mesmo
envelope.

## Reprocessar

Reimportar é seguro e é o procedimento padrão de recuperação: um import
substitui exatamente o seu próprio `(provider, dia de origem)`, então rodar de
novo converge para o mesmo estado em vez de duplicar linhas. Há teste para isso
(`TestReimportingAScenarioIsIdempotent`).

Em produção, o envelope fica no S3: re-subir o objeto redispara a Lambda. Falhas
saem no log com `bucket` e `key` — veja [deploy.md](deploy.md#recovering-a-failed-payment-import).

## O que ainda não é importado

Só o evento `1` (venda/pagamento) vira dado canônico. Cancelamentos, chargebacks
e ajustes (eventos 3, 5, 6, 26, 27…) são ignorados pelo parser. O extrato
`cashouts` é carregado no envelope e arquivado, mas nada o lê ainda — saques para
a conta bancária não fazem parte do domínio canônico.
