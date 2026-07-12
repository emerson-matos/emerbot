# ADR-003: OpenTofu para IaC

## Status

Accepted

## Contexto

Toda infraestrutura deve ser reproduzível e versionada.

## Decisão

Provisionar a infraestrutura exclusivamente com OpenTofu.

## Consequências

- rastreabilidade das mudanças
- reprodutibilidade entre ambientes
- necessidade de pipeline de build/deploy para artefatos da Lambda

