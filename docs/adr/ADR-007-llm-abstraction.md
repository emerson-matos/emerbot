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

## Nota de implementação (atualização)

O contrato acabou materializado como `orchestrator.TextGenerator`
(`packages/orchestrator/textgen.go`) — a interface `Client` em `packages/llm`
descrita originalmente **nunca foi criada**; esse é o seam real de provider.
`orchestrator.NewTextGenerator` (`packages/orchestrator/service.go`) seleciona a
implementação. Providers:

- **Gemini** (`packages/orchestrator/internal/gemini`) — provider de produção.
- **Ollama** (`packages/orchestrator/internal/ollama`) — provider local/dev
  (ver ADR-012).
- **StaticClient** (`packages/orchestrator/static.go`) — fallback sem LLM.

O system prompt do agente vive em `packages/orchestrator/internal/agentprompt`,
compartilhado pelos providers para não divergir a persona ao trocar de modelo.

A decisão da ADR (não acoplar estruturalmente a um provider) segue válida; muda
apenas o nome/local do contrato. Os tools ainda são tipados em `*genai.Schema`
(`packages/finance/tools.go`) e convertidos por provider — desacoplar esse tipo é
um passo futuro.
