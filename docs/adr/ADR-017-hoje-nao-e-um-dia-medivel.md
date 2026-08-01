# ADR-017: Hoje não é um dia medível

## Status

Accepted

## Contexto

No dia 1º de agosto, às 9h, o notificador mandou isto:

> **⚠️ Pendências importantes:** Assinatura do app painel financeiro: vencida em
> 31/07. Folha de pagamento: vencida em 30/07.
> **📊 Panorama do mês:** Saúde financeira: Crítica. Fluxo de caixa: Negativo.
> Receita: houve uma queda de 100% em relação ao mês passado. Recomendamos uma
> análise imediata das causas.

As duas pendências eram verdadeiras. Todo o resto era artefato de calendário. A
farmácia nem tinha aberto.

Três cálculos independentes produziram o mesmo erro, e todos pela mesma causa:

1. **Comparação mês a mês.** `buildComparison` truncava os dois meses no
   `now.Day()` — no dia 1º, isso é uma manhã vazia contra um dia inteiro de
   julho. Queda de 100%, todo santo dia 1º. Uma correção anterior (ADR implícito
   em `buildComparison`) já tinha resolvido o caso grosseiro de "mês parcial
   contra mês fechado", mas manteve a distorção menor — justamente a que é pior
   no horário em que o resumo é enviado.
2. **Saúde do mês.** `buildHealth` julgava o mês pelo `ExpectedBalance` do
   resumo mensal, que cobre o mês **inteiro**, inclusive o que está apenas
   *agendado*. No dia 1º isso é todo o aluguel, toda a folha e todo o
   fornecedor do mês contra zero de venda: negativo por construção, e "crítico"
   por consequência. Todo mês, sem exceção.
3. **Ritmo semanal.** `WeekComparison.PreviousUpToDay` comparava esta semana
   *incluindo hoje* com o mesmo dia da semana passada *inteiro*. Na segunda-feira
   de manhã: queda de 100%.

E o espelho disso, do outro lado: `DaysRemaining` era `diasDoMês - hoje`, e a
projeção somava as médias a partir de **amanhã**. Ou seja, hoje era contado como
um dia já gasto na hora de cobrar resultado e como um dia já perdido na hora de
projetar. No último dia do mês o painel dizia "não há mais dias para recuperar",
com a loja aberta.

Um alerta que grita todo dia 1º ensina as pessoas a ignorar o alerta.

## Decisão

Existe uma linha, e ela é a mesma para toda a análise:

- **O que é medido termina ontem.** Hoje ainda está acontecendo; não é um ponto
  de dado. Comparação mês a mês, ritmo da semana, médias por dia da semana,
  "dias com movimento" e o veredito de saúde só olham dias **fechados**.
- **O que é cobrado começa hoje.** Hoje é um dia em que ainda dá para vender.
  Ele entra em `DaysRemaining` e na projeção — descontando o que já foi vendido
  hoje, para não contar o dia duas vezes.

A linha é decidida uma vez, em `monthClock` (`packages/finance/analytics/clock.go`),
e exposta no payload como `Analysis.Period`. Ela é derivada do **mês analisado**,
não do mês atual: um julho fechado, aberto em agosto, não tem "30 dias
restantes".

`Period.ThroughDay == 0` com `InProgress` é o primeiro dia do mês: **não há nada
atrás de nós**. Nesse caso a comparação não é zero e não é queda — ela não
existe. Os dois lados ficam zerados, `buildTrends` devolve tendências planas,
`hasBaseline` derruba as recomendações mês a mês, e o veredito de saúde é
substituído por um único insight `month_start`. Quem consome tem de dizer "o mês
está começando", nunca desenhar uma queda a zero.

O resumo do WhatsApp passa a ter duas metades explícitas, nessa ordem:

```
⏭️ A partir de agora:      (AheadLines + os alertas com prazo)
📊 Como fechamos até ontem: (DigestLines)
```

A ordem é deliberada: a mensagem chega de manhã, e o que dá para fazer vale mais
do que o diagnóstico. No dia 1º a segunda metade é uma linha só — "o mês está
começando" — e a primeira continua inteira.

## Consequências

- `SchemaVersion` vai a **3**. Saíram `kpis.daysRemaining` e
  `kpis.previousMonthRevenueUpToDay` (agora `period.daysRemaining` e
  `trends.faturamento`, que já traz os dois meses sobre os mesmos dias
  fechados); `weekComparison.previousUpToDay` virou `weekComparison.pace`.
  O snapshot diário do dia da subida é recusado pelo dashboard-api até o
  notificador rodar de novo — que é o comportamento já definido em
  `snapshot.go`, e é preferível a comparar campos que mudaram de sentido.
- Percentuais agora carregam a ressalva "(até o dia N)" com N = **ontem**. Os
  prompts do notificador e do agente exigem que o modelo repita a ressalva: foi
  ele que transformou "100% abaixo do mês passado (até o dia 1)" em "queda de
  100% em relação ao mês passado".
- As médias por dia da semana ficam ligeiramente mais altas durante o dia, por
  não diluírem mais o dia em curso.
- A regra "não mando nada se não houver alerta" continua valendo. Um dia sem
  pendência e sem meta segue sem mensagem.
