# ADR-001: Arquitetura Serverless

## Status

Accepted

## Contexto

O projeto possui restrição forte de custo e precisa evitar infraestrutura permanente.

## Decisão

Usar API Gateway, Lambda, DynamoDB e serviços gerenciados com cobrança por uso.

## Consequências

- custo previsível e baixo em baixa escala
- menor carga operacional
- necessidade de projetar para cold starts, idempotência e observabilidade

