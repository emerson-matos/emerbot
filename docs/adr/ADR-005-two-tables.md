# ADR-005: Duas tabelas para memória

## Status

Accepted

## Contexto

Curto prazo e longo prazo possuem retenção e semântica diferentes.

## Decisão

Separar `Messages` e `Memories` em duas tabelas.

## Consequências

- políticas de retenção independentes
- menor ambiguidade semântica
- queries mais simples para cada caso de uso

