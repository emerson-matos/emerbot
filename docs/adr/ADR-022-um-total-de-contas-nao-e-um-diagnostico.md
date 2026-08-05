# ADR-022: Um total de contas a vencer não é um diagnóstico

## Status

Accepted

## Contexto

Dez minutos depois da meia-noite de uma quarta, perguntado "como estamos", o bot
fechou assim:

> **Saúde Financeira:** O mês começou no azul, mas o volume de despesas
> agendadas (**R$ 19.130,95**) é um ponto de atenção. Lembre-se que esses valores
> são compromissos futuros e não devem ser somados ao que já foi gasto.

A segunda frase é a regra do prompt sendo obedecida à risca. A primeira é uma
conclusão que ninguém autorizou.

Três coisas estavam erradas ao mesmo tempo:

1. **O veredito de saúde é do backend, e ele não conta contas agendadas.** O
   ADR-017 tirou `ExpectedBalance` do `buildHealth` exatamente porque julgar um
   mês pelas contas que ainda vão vencer o declarava crítico todo dia 1º. O
   modelo devolveu a preocupação para dentro da seção "Saúde Financeira" — a
   mesma conta, por fora do código.

2. **O caixa daquele mês nunca chegou perto de zero.** `dias_ate_saldo_negativo`
   era nulo, `menor_saldo_projetado` era positivo, e o próprio bot tinha dito
   isso numa resposta anterior. Ele tinha a resposta e mesmo assim tratou o
   total como ameaça.

3. **`despesa_agendada` estava no lugar errado do payload.** Ficava no topo,
   entre `despesa` e `resultado` — as figuras de *como o mês foi*. Um número
   nessa vizinhança se lê como desempenho, e desempenho pede julgamento.

O padrão é o mesmo do ADR-019: **um dado sem a pergunta que ele responde vira
matéria-prima para o modelo inventar uma.** Lá era a divisão do gap pelos dias;
aqui é um total grande sem nada contra o que pesá-lo.

## Decisão

**Um compromisso é questão de liquidez, nunca de desempenho — e ele viaja com a
resposta dele.**

No `ToolPayload`, `despesa_agendada` sai do topo e vira `caixa.compromissos_do_mes`,
ao lado do runway que julga se há dinheiro para eles. Junto vem
`caixa.compromissos_situacao` (`CashPosition.Commitments`), com três valores:

| valor | quando | o que o consumidor faz |
| --- | --- | --- |
| `coberto` | a curva projetada nunca fica negativa | diz que está coberto, sem alarme |
| `descoberto` | a curva mergulha em algum ponto do mês | avisa, com `menor_saldo_projetado` e `dias_ate_saldo_negativo` |
| `sem_historico` | sem histórico para creditar os dias à frente | diz que não dá para responder — nunca que as contas são impagáveis |

O veredito sai do **fundo do poço** (`LowestProjected < 0`), não de
`DaysUntilNegative`: aquele campo só conta travessias ainda à frente, então um
saldo que já está negativo hoje seria classificado como `coberto` — justamente o
caso em que dizer isso é pior.

E é derivado da curva, não do valor: o que torna R$ 20.000,00 de contas um
problema não é o tamanho delas, é o saldo não cobrir. `CommitmentCoverage` não
guarda dinheiro nenhum, só a leitura.

Duas regras novas no prompt, e a segunda é a que faltava:

- o total sozinho nunca é problema; a resposta está em `compromissos_situacao`;
- **a saúde do mês é o veredito de `health.status`/`health.messages`, e o modelo
  não acrescenta preocupações a ela.** Contas a vencer não entram: são caixa,
  não desempenho.

## Consequências

- `SchemaVersion` **não sobe**. `KPIs.DespesaAgendada` continua exatamente onde
  está, com o mesmo significado — a página Análise segue lendo "+ R$ X a vencer"
  dela. O que mudou de lugar foi a chave do `ToolPayload`, que é uma projeção
  curada da `Analysis` e não a `Analysis`. `CashPosition.Commitments` é campo
  novo e derivado; um snapshot antigo lido nesta struct traz `""`, que não é
  nenhum dos três valores e portanto não afirma nada — o consumidor cai no
  mesmo silêncio que já tinha antes do campo existir, e nenhuma leitura do diff
  passa por aqui.
- O `ToolPayload` é o lugar certo para agrupar por pergunta. Ele já renomeia,
  converte para reais e omite o que a página desenha; juntar um número à
  resposta dele é a mesma função.
- Fica registrado o teste que vale para qualquer campo novo do payload:
  **se um número pode ser lido como veredito, ele precisa vir com o veredito.**
  Não basta o dado estar certo e a ressalva estar escrita — a ressalva estava, e
  o modelo a repetiu corretamente na mesma frase em que tirou a conclusão que
  ela não sustenta.
