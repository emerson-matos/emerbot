# ADR-020: Amanhã também é um dia com meta

## Status

Accepted — a decisão vale; a **forma** foi corrigida pelo ADR-021 antes de
qualquer parte disto chegar ao `main`. O campo `meta_de_amanha` descrito abaixo
não existe: um campo por dia não sobrevive à pergunta seguinte ("e no sábado?"),
e o dia virou parâmetro de `get_meta_do_dia`. O `SchemaVersion` 8 anunciado aqui
é o mesmo salto único que o 021 entrega — não são duas versões.

## Contexto

Em 5 de agosto, no fim do expediente, com o faturamento do dia já lançado, o bot
foi perguntado como a farmácia estava projetada para o dia seguinte. Respondeu
isto:

> Para amanhã, dia 06/08/2026 (quarta-feira), **não temos uma meta específica de
> faturamento calculada**, mas podemos olhar para o histórico: a média histórica
> de faturamento para as quartas-feiras é de **R$ 1.028,29**.
>
> Em termos de caixa, a projeção é que o mês encerre com um saldo positivo de
> **R$ 12.497,16** (…) Temos **R$ 20.096,97** em despesas agendadas para o
> restante do mês.

Nada ali é falso. E nada ali responde à pergunta.

**Não havia meta para amanhã porque nada no payload carregava uma.** O ADR-019
tinha acabado de resolver o problema de o modelo inventar um valor por dia, e
resolveu do jeito certo: `meta_de_hoje`, a fatia de hoje no que falta, escalada
pelo ritmo do próprio dia da semana. Mas a fatia era só de hoje. O plano que
precifica amanhã — o mesmo `factor`, aplicado à média da quarta — já existia
dentro de `buildProjection` e não saía de lá.

Sobrava ao modelo o campo mais próximo, `media_por_dia_da_semana`. E ele fez o
que qualquer um faria: leu a média em voz alta. Só que **a média não é a meta**.
R$ 1.028,29 é o que uma quarta-feira *costuma* faturar; a pergunta era o que
*esta* quarta-feira precisa faturar. Num mês adiantado as duas divergem para
baixo, num mês atrasado divergem para cima, e é exatamente no mês atrasado que
alguém pergunta. Apresentar uma como a outra é o mesmo erro do ADR-019 — um
número que descreve o passado usado como alvo —, só que dessa vez o bot nem
tinha como saber que estava cometendo.

Pior é a frase que veio antes: "não temos uma meta específica calculada". O
sistema tinha. Estava a um campo de distância.

O lado do caixa errou pelo mesmo motivo, em outra escala. Perguntado sobre **um
dia**, o payload só sabia falar do **mês**: `projecao_fim_do_mes`,
`menor_saldo_projetado`, `dias_ate_saldo_negativo`, `despesa_agendada`. Daí a
resposta ter terminado com R$ 20.096,97 de compromissos do mês inteiro
apresentados como "lembrete importante" a quem tinha perguntado como estava
amanhã. O número é real e é grande; fora de contexto, é só alarme.

E havia uma terceira ausência, que só aparece na hora em que a pergunta foi
feita: **o payload não dizia quanto o dia tinha vendido.** `meta_de_hoje` é uma
meta de dia inteiro, medida a partir da manhã (ADR-019); o único faturamento ao
lado dela era o do mês, que já inclui hoje. Não havia subtração honesta entre os
dois, então "bati a meta de hoje?" — a pergunta que naturalmente antecede "e
amanhã?" — não tinha resposta.

## Decisão

**A meta de um dia não é privilégio de hoje.** O que o ADR-019 estabeleceu para
hoje vale para qualquer dia que a farmácia ainda vai abrir dentro do mês
analisado, e amanhã é o único outro que alguém pergunta.

O cálculo passa a ser um só, explícito: `remainingPlan` distribui o que falta
sobre os dias restantes, cada um pelo seu próprio ritmo semanal. Todos os dias
saem do **mesmo `factor`**, com a propriedade que uma média plana nunca teve:

```
Σ média[dia_da_semana(d)] × factor, para d de hoje até o fim  ==  o que falta
```

`TodayTarget` e `NextDayTarget` são duas fatias desse plano, não duas previsões
concorrentes. Isso tem uma consequência que precisa ser dita em voz alta: **a
meta de amanhã pressupõe que hoje feche na meta de hoje.** É a mesma hipótese
que `Projection.Projected` já fazia sobre hoje ao creditá-lo com a média do seu
dia da semana. Condicionar amanhã só ao que já está lançado seria mais preciso
às 22h e grosseiramente pessimista às 8h — e, pior, faria o mesmo payload dizer
duas coisas diferentes sobre hoje, que é a classe de divergência que os ADRs
anteriores existem para matar.

Quatro campos sustentam a decisão:

1. **`meta_de_amanha`**, com a mesma forma de `meta_de_hoje`: a meta ao lado da
   `media_historica` daquela quarta, porque um número sozinho não tem tamanho.

2. **`faturamento_de_hoje`**, para que a meta do dia tenha contra o que ser
   batida. Sem ele o fechamento do dia dependia de o modelo subtrair hoje do mês
   — uma conta que ele não tem como fazer.

3. **`caixa.amanha`**: saldo projetado, entradas agendadas, entradas esperadas e
   despesas agendadas **daquele dia**. Nem `projecao_fim_do_mes` nem
   `despesa_agendada` descrevem amanhã, e foi respondendo com eles que o bot
   citou vinte mil reais a quem perguntou sobre um dia.

4. **`data` em toda meta de dia**, inclusive nas que não têm valor. O modelo
   resolve "amanhã" contra o prompt do sistema; a análise resolve contra o
   `monthClock`. Se as duas não dizem a mesma data em voz alta, ninguém percebe
   quando divergem.

O estado ganha um motivo novo, `mes_acaba_hoje`: no dia 31, amanhã é de um mês
com meta própria, gap próprio e histórico próprio, e esta análise não viu
nenhum dos três. Dizer "sem_meta" seria uma afirmação sobre o mês que vem. O
campo diz onde a pergunta cai, e o prompt manda repetir isso.

A proibição do ADR-019 continua e é estendida no prompt: perguntas sobre amanhã
se respondem com `meta_de_amanha`, **nunca** com `media_por_dia_da_semana`. Como
lá, a regra negativa vem sempre acompanhada do campo que responde — porque um
modelo sem número para dar inventa um, e desta vez o que ele inventou foi uma
média histórica com cara de meta.

## Consequências

- `TodayTarget` virou `DayTarget`: o tipo descreve um dia, e o dia não é mais
  necessariamente hoje. Os campos JSON (`todayTarget`) e os valores de estado
  não mudaram; mudaram os identificadores em Go e TypeScript.
- `SchemaVersion` vai a **8**. As quatro mudanças são adições, pelas quais esta
  lista normalmente não sobe. Ela sobe pelo mesmo motivo da versão 7: um
  snapshot v7 lido nesta struct produz um `nextDayTarget` com `state` vazio, e
  vazio é a única coisa que `DayTargetState` não significa — todo valor real
  nomeia uma meta ou o motivo de não haver uma. Uma ausência sem nome é
  exatamente o que o campo foi criado para impedir.
- O painel ainda não desenha a meta de amanhã. Isso é ausência, não divergência:
  o número é único e vem do mesmo plano, e quando a tela quiser mostrá-lo já
  está no payload que ela recebe.
- Fica registrado o limite que o ADR-019 não alcançou: **dar acesso ao dado
  certo para a pergunta de ontem não cobre a pergunta de amanhã.** O ADR
  anterior fechou a única conta errada que o modelo sabia fazer e deixou aberta
  a pergunta seguinte, sem campo e sem regra. Um payload que responde bem "quanto
  hoje?" e mal "quanto amanhã?" não tem um problema de prompt — tem um campo
  faltando.
