# ADR-011: Unificação do parser financeiro em agente com function calling

## Status

Aceito — implementado (Fase 2 da migração Gemini).

## Contexto

O parser financeiro (ADR-010) extrai lançamentos de mensagens WhatsApp enviando
texto ao Gemini com `responseSchema` e recebendo JSON estruturado. O modelo não
sabe a data atual, então expressões como "último dia do mês" são interpretadas
com data errada.

Além disso, o parser só trata um caso de uso: **registrar** lançamentos.
Perguntas como "como estamos este mês?" ou "quanto falta receber?" caem no
fallback genérico ("Sou um assistente financeiro").

Queremos evoluir o bot para que o Gemini também responda consultas ao DynamoDB
— sem criar um segundo fluxo paralelo.

## Decisão

Substituir o `GeminiParser` (resposta JSON única) por um `GeminiAgent` que usa
**function calling** do Gemini para tudo: criar lançamentos e consultar dados.

### Fluxo novo

```
WhatsApp → financial.Handler
  ├─ slash command (/despesa, /pagar…) → RegexParser (fast path, sem LLM)
  └─ linguagem natural → GeminiAgent
       ├─ create_financial_entry()      → registra lançamento
       ├─ get_month_summary()           → resumo do mês
       ├─ list_due_entries()            → contas a pagar/receber
       └─ search_entries()              → busca lançamentos
       └─ (nenhuma tool)                → resposta textual ("sou financeiro")
```

O agente faz um loop de até 5 turnos: se o Gemini retorna um `FunctionCall`, a
Lambda executa a tool localmente contra o DynamoDB e envia o resultado de volta.
Quando o Gemini retorna texto puro, o loop termina e a resposta é enviada ao
usuário.

### Tools da POC

Serão quatro, todas implementadas como funções Go que recebem `finance.Store`:

1. **`create_financial_entry`** — cria um lançamento no DynamoDB (substitui o
   JSON intermediário do parser atual)
2. **`get_month_summary`** — retorna receitas, despesas e saldo do mês
3. **`list_due_entries`** — lista contas pendentes em um período
4. **`search_entries`** — busca por descrição, categoria ou período

### Prompt dinâmico com data

Independente das tools, o system prompt do agente inclui:

```
Hoje é 21/07/2026.
Fuso horário: America/Sao_Paulo.
```

Isso resolve o bug de datas relativas sem depender de tool `get_context()`.

### Fast path de regex mantido

Comandos com barra (`/despesa 500 aluguel`) continuam resolvidos localmente —
gratuito e mais rápido. Só texto livre (sem `/`) vai para o agente.

## Consequências

- **Positivas**
  - Um único componente para registro e consulta
  - Datas relativas funcionam (prompt tem a data atual)
  - Arquitetura preparada para novas tools (saldo, categorias, etc.)
  - Slash commands continuam rápidos e independentes de LLM

- **Negativas**
  - Duas chamadas Gemini para consultas com tool (tool call + tool response)
  - Complexidade maior que o parser atual (loop de function calling)
  - Precisa adicionar filtro por descrição no `EntryFilter` do DynamoDB

- **Neutras**
  - `GeminiParser` e a interface `Parser` são removidos
  - `GeminiAgent` substitui ambos para o fluxo de linguagem natural
  - `RegexParser` permanece, mas como implementação concreta, não via interface

## Referências

- ADR-010: Parser de linguagem natural com Gemini (substituído)
- ADR-006: Orchestrator central (inspiração para o design de tools)
