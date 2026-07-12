# ADR-007: Abstração obrigatória de LLM

## Status

Accepted

## Contexto

O projeto não pode depender estruturalmente de um provider específico.

## Decisão

Todo provider deve implementar a interface `Client` em `packages/llm`.

## Consequências

- troca simplificada de provider
- testes mais simples com doubles
- necessidade de contratos internos claros de entrada e saída
