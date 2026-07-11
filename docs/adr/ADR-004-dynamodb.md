# ADR-004: DynamoDB como banco principal

## Status

Accepted

## Contexto

O sistema exige custo baixo, alta disponibilidade e modelo orientado por padrão de acesso.

## Decisão

Usar DynamoDB com tabelas `Messages` e `Memories`.

## Consequências

- custo sob demanda
- alta disponibilidade gerenciada
- necessidade de modelagem disciplinada por `Query`, nunca `Scan`

