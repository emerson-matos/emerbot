# ADR-002: Go como linguagem principal

## Status

Accepted

## Contexto

O runtime precisa ter baixo cold start, baixo consumo de memória e boa experiência em Lambda.

## Decisão

Go será a linguagem principal do monorepo.

## Consequências

- binário único e simples de empacotar
- ótima adequação para Lambda
- menor flexibilidade que linguagens dinâmicas em experimentação rápida

