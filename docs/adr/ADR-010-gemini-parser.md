# ADR-010: Parser de linguagem natural com Gemini

## Status

Accepted

## Contexto

O parser financeiro do WhatsApp (`packages/whatsapp.Parser`) só entendia
comandos com barra (`/despesa`, `/pagar` etc.) via regex. Mensagens em
linguagem natural ("paguei 500 de aluguel ontem") caíam no orquestrador
genérico (`llm.StaticClient`), que não é um LLM real — não extrai lançamentos.
Já existia um `GeminiParser` no código, mas sem testes, apontando para o
modelo `gemini-2.0-flash`, aposentado em 03/03/2026, e "convencendo" o modelo a
responder JSON só por instrução de prompt (sem `responseSchema`).

## Decisão

- **Escopo**: o Gemini só entra no parser financeiro (extração de
  lançamentos). O orquestrador de chat continua no `StaticClient` — sem LLM
  real ali, para não estourar o teto de custo (ADR-008).
- **Modelo**: `gemini-2.5-flash-lite`, pelo tier gratuito mais generoso da
  linha (15 RPM / ~1000 req/dia) e por ser suficiente para uma tarefa de
  extração estruturada — não precisa do modelo mais caro.
- **Saída estruturada**: `responseSchema` (`responseMimeType:
  application/json`) em vez de só pedir "responda com JSON" no prompt. Isso
  reduz respostas malformadas e permite remover a instrução redundante do
  prompt.
- **Guarda `is_financial`**: campo novo e obrigatório no schema. Se a
  mensagem não for um lançamento/comando financeiro (saudação, pergunta,
  papo), o parser retorna `ErrNotFinancial` em vez de inventar um lançamento
  — o handler responde com uma mensagem amigável apontando para `/help`.
- **Fast-path de regex mantido**: comandos com barra continuam resolvidos
  localmente, sem chamar o Gemini — grátis e mais rápido.
- **Fallback**: qualquer falha do Gemini (erro de rede, timeout de 10s,
  resposta vazia/malformada) devolve o erro normal de parse, que o handler já
  trata como tutorial de uso. Se a API key não estiver configurada, ou se
  `NewGeminiParser` falhar, o app volta para o `RegexParser`.
- **Sem mudança de infra**: `GEMINI_API_KEY` já é injetada como env var pelo
  OpenTofu (ADR-007/008); esta decisão é só de código.

## Consequências

- lançamentos em linguagem natural passam a funcionar sem exigir sintaxe de
  comando
- menos risco de lançamento fantasma (guarda `is_financial`)
- saída mais confiável, menos parsing frágil de markdown/JSON solto
- dependência de disponibilidade do Gemini para texto livre — mitigada pelo
  fallback ao tutorial e pelo fast-path de regex, que não depende da API
- custo adicional desprezível dentro do tier gratuito, coerente com ADR-008
