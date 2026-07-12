# ADR-006: Orchestrator central

## Status

Accepted

## Contexto

O fluxo não deve ser delegado ao LLM.

## Decisão

Centralizar coordenação de memória, tools e LLM em `packages/orchestrator`.

## Consequências

- fluxo explícito e testável
- fácil controle de autorização e observabilidade
- responsabilidade arquitetural concentrada em um ponto do sistema

