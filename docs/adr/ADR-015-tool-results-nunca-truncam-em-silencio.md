# ADR-015: Resultados de ferramenta nunca truncam em silêncio

## Status

Accepted

## Contexto

Perguntando no WhatsApp "quanto temos que pagar começando no dia 1/8 até 31/08,
some e agrupe por categorias", o bot respondeu com um total de R$ 5.636,83 e um
detalhamento de exatamente 20 linhas, todas entre 21/08 e 31/08. O total estava
errado: faltava tudo de 01/08 a 20/08. Nada na resposta indicava isso — a lista
parecia completa, o total parecia calculado, e o usuário não tinha como
desconfiar.

A causa não era o modelo. `list_due_entries` e `search_entries` devolviam um
array puro de lançamentos, com `limit` caindo no padrão de 20 (`clampLimit`), e
`ListEntries` ordena por data efetiva decrescente e corta o excedente. O modelo
recebeu, sem nenhum sinal de corte, os 20 vencimentos mais recentes de agosto,
somou-os e apresentou o resultado como o total do mês.

Duas decisões de design se somaram para produzir o erro:

1. **A ferramenta terceirizava a aritmética para o modelo.** Não havia como
   pedir "some e agrupe por categoria" — só listar lançamentos crus e deixar o
   LLM somar. `Store.CategorySummary` já existia e fazia exatamente isso, mas
   nenhuma ferramenta a expunha.
2. **O corte era invisível.** Um array de 20 elementos é indistinguível de "só
   existem 20". A resposta da ferramenta não tinha vocabulário para dizer
   "existem mais".

Num assistente financeiro isso é pior que um erro: um total errado com cara de
certo entra na decisão de pagamento do dia. "Sem dados" e "dados cortados"
não podem renderizar igual — a mesma regra que ADR-014 aplica a datas
malformadas.

## Decisão

As ferramentas de listagem devolvem um envelope, não um array:

```json
{
  "entries": [...],
  "count": 31,
  "truncated": false,
  "totals_available": true,
  "total_matching": 31,
  "total_expense": 5636.83,
  "total_entradas": 0,
  "total_faturamento": 0,
  "by_category": [{"category": "...", "label": "...", "total": 4636.83, "count": 9}],
  "period": {"from": "2026-08-01", "to": "2026-08-31"}
}
```

`truncated` só aparece como `true` nos dois casos abaixo, e aí vêm junto
`omitted` e `warning`.

Com quatro garantias:

- **Um período é o seu próprio limite.** Quando `from` e `to` são informados, a
  consulta devolve o período INTEIRO — `limit` não se aplica, é ignorado e não
  apenas clampado. Colocar um segundo limite arbitrário em cima de um intervalo
  que o usuário já delimitou é precisamente o que fez uma pergunta sobre agosto
  ser respondida com dois terços de agosto. `limit` só significa alguma coisa
  numa consulta sem período.
- **Os totais cobrem o período, não a página.** `total_expense`, `total_entradas`, `total_faturamento`
  e `by_category` são calculados sobre todos os lançamentos da janela, então o
  modelo nunca precisa somar nada — e o prompt proíbe que ele some.
- **Qualquer corte é declarado.** Se ainda assim alguma linha ficar de fora
  (consulta sem período, ou um intervalo além de `maxRangeEntries`),
  `truncated`, `omitted` e `warning` dizem isso em voz alta, e o prompt exige
  que o modelo repasse ao usuário.
- **Sem período não há total.** Uma consulta sem `from`/`to` não tem como ser
  somada honestamente sem ler o ledger inteiro, então devolve
  `totals_available: false` e um `note` pedindo as datas — em vez de uma soma
  parcial.

`maxRangeEntries` (500) não é um tamanho de página: é um anteparo para que um
ledger patológico não seja despejado inteiro num prompt só. No volume da
farmácia um ano de lançamentos fica muito abaixo disso, então na prática ele
nunca dispara — e se disparar, dispara ruidosamente. O padrão de `limit` (agora
só para consultas sem período) subiu de 20 para 50, teto de 100 → 200.

### Como identificar o problema de novo

Um corte agora deixa rastro em três lugares, do mais barato ao mais caro:

1. **Na própria resposta ao usuário.** O modelo é instruído a dizer "mostrando
   X de Y lançamentos". Se a resposta não menciona corte, não houve corte.
2. **No CloudWatch.** `listing` loga
   `finance tool <nome>: truncated result for user <id>: showing N of M entries`
   toda vez que omite linhas. Um relato de "o total parece errado" se checa
   filtrando esse texto no log da Lambda do webhook, em vez de se adivinhar.
3. **Nos testes.** `TestStoresAgreeOnListDueEntriesTotals` roda o cenário de
   agosto contra as duas implementações de `Store`, então uma divergência entre
   DynamoDB e memória vira teste vermelho e não bug só-em-produção.

## Consequências

- Uma consulta com período lê e devolve a janela inteira em vez de parar num
  limite. Numa farmácia com dezenas de lançamentos por mês isso é irrelevante
  em custo de leitura e em tamanho de prompt, e é o único jeito de o total
  estar certo. `maxAggregateSpanDays` (366) impede que um intervalo alucinado
  ("de 2000 a 2100") seja confundido com uma pergunta sobre um período e vire
  varredura da partição toda; nesse caso a consulta cai no caminho paginado,
  sem totais.
- A forma do retorno mudou: quem consumia o array precisa ler `entries`. Só as
  duas ferramentas usavam essa forma.
- `foldByCategory` passa a ser a definição única de "agrupado por categoria",
  usada tanto pelo envelope quanto por `CategorySummary` — o mesmo princípio de
  ADR-014 de escrever comportamento compartilhado uma vez só.
