# ADR-012: Provider LLM local com Ollama (dev)

## Status

Aceito — implementado.

## Contexto

No ambiente local (`make demo`) o webhook não recebe `GEMINI_API_KEY`, então o
`orchestrator` cai no `StaticClient` (respostas fixas). Isso impede testar de
verdade o fluxo de chat e a memória de curto prazo (ADR-005) sem gastar a chave
da Gemini nem depender de rede.

Queremos rodar um **modelo open source localmente** para desenvolvimento,
mantendo os tools financeiros (function calling) — a mesma experiência da Gemini,
só que offline e sem custo.

A costura de provider já existe: `orchestrator.TextGenerator`
(`packages/orchestrator/textgen.go`), escolhida em `NewTextGenerator`
(`packages/orchestrator/service.go`). Os tools são provider-agnósticos em
`finance.FinanceTools(store)` (`packages/finance/tools.go`). O SDK `genai` só
fala com Gemini/Vertex, então um modelo local exige um segundo caminho de agente.

## Decisão

Adicionar um **segundo agente (Ollama)** atrás do mesmo `TextGenerator`,
selecionado por env, **apenas para dev/local**. A Gemini continua o provider de
produção — nenhuma infra AWS muda.

- Novo `packages/orchestrator/internal/ollama` conversa via HTTP com o Ollama
  (`POST /api/chat`, `stream:false`), sem dependência nova (`net/http`).
- Reusa `finance.FinanceTools(store)`; converte `*genai.Schema` → JSON Schema
  on-the-fly. O loop de function calling espelha o do agente Gemini.
- `NewTextGenerator` passa a escolher `ollama → gemini → static`, por
  `LLM_PROVIDER`/`OLLAMA_HOST`/`OLLAMA_MODEL`.
- O Ollama sobe no docker-compose **como opt-in** (compose profile), porque o
  modelo pesa ~5GB e o disco de dev é apertado; o `make demo` padrão fica leve.
- Modelo default `llama3.1:8b` (tool-capable); `qwen2.5:7b` como alternativa.

## Consequências

- **Positivas**
  - Chat + memória testáveis localmente com um LLM real, sem chave nem custo.
  - Segundo provider valida na prática a abstração da ADR-007 (o seam real é
    `TextGenerator`, não o `packages/llm.Client` que nunca foi criado).
  - Nenhum impacto em produção (opt-in, só dev).

- **Negativas**
  - Inferência em CPU é lenta (segundos por resposta) e o 1º run baixa ~5GB.
  - Qualidade de function calling de modelos locais < Gemini.
  - Duas implementações de agente para manter (Gemini e Ollama) enquanto os tools
    seguirem tipados em `*genai.Schema`.

- **Neutras**
  - Timeout do agente local é maior que o da Gemini (o webhook local roda como
    servidor HTTP comum, sem o teto de 10s do Lambda).

## Referências

- ADR-007: Abstração obrigatória de LLM (o seam efetivamente usado)
- ADR-005: Memória de curto prazo (o que passa a ser testável localmente)
- ADR-011: Agente Gemini com function calling (espelhado pelo agente Ollama)
