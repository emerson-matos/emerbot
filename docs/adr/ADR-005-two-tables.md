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

## Status de implementação

- **Messages (curto prazo)** — implementado na Fase 1 como a tabela
  `${prefix}-conversations` (`packages/conversation`): um item por turno,
  hash key `PK` (telefone) + range key `SK` cronológico, TTL em `ExpiresAt`. O
  webhook grava cada turno e carrega o histórico recente, que o orchestrator
  injeta no prompt do Gemini (`gemini.Agent.Process`).
- **Memories (longo prazo)** — ainda não implementado. O tipo `domain.Memory` e a
  `LongTermStore` existem, mas nada popula os fatos ainda (Fase 2: extração via
  tools `remember_fact`/`forget_fact` ou resumo periódico).

