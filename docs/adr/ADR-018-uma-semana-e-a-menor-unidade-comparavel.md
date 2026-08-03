# ADR-018: Uma semana é a menor unidade comparável

## Status

Accepted

## Contexto

No dia 3 de agosto, o painel mostrou estas duas recomendações:

> **Receita caiu** — 22% abaixo do mês passado (até o dia 2). Identifique causas
> e aja rapidamente.
> **Saldo fica negativo em breve** — O saldo fica negativo em 1 dia. Reduza
> despesas ou antecipe recebimentos.

Nenhuma das duas era sobre a farmácia.

**A queda de 22%.** Os dias 1 e 2 de agosto de 2026 são sábado e domingo. Os
dias 1 e 2 de julho são quarta e quinta. O ADR-017 já tinha alinhado os dois
lados no mesmo *número do dia* — o que resolveu comparar meia agosto com julho
inteiro — mas o número do dia não é a unidade em que uma farmácia funciona. Uma
janela de N dias a partir do dia 1º só carrega a mesma composição de dias da
semana nos dois meses quando **N é múltiplo de 7**. Em qualquer outro tamanho um
dos meses ganha um sábado a mais que o outro, e a diferença é máxima justamente
quando a janela é menor — na primeira semana, todo mês.

O teste que fixa isso é direto: uma farmácia que fatura no sábado e no domingo
de agosto **exatamente** o que faturava em todo fim de semana de julho é
reportada com "Receita caiu 53% abaixo do mês passado", em vermelho. Não é
amostra pequena sendo honesta; é amostra enviesada, e o viés tem sinal
conhecido.

**O saldo negativo em 1 dia.** `cashFlowForecast` soma só o que já está
lançado. Os dois lados do mês de uma farmácia são registrados em momentos
opostos: aluguel, folha e fornecedor entram no dia 1º para o mês inteiro,
enquanto uma venda só existe quando acontece. A curva depois de hoje é despesa
sem contrapartida, e mergulha. É a mesma falha que o ADR-017 encontrou no
veredito de saúde — julgar o mês por contas que estão apenas *agendadas* —
sobrevivendo na única leitura que ele não alcançou.

Os dois casos são o mesmo erro visto de dois ângulos: **o dia 1º de um mês não é
um fato sobre a farmácia.** O ADR-017 traçou a linha em "hoje"; faltava traçá-la
na virada do mês.

## Decisão

**A comparação mês a mês fecha em semanas inteiras.** `monthClock` passa a
responder duas perguntas em vez de uma:

- `through` — o que aconteceu aqui. Último dia fechado do mês. É o que sustenta
  o semáforo, o resultado, "dias com movimento", a razão despesa/faturamento.
- `comparableThrough` — o que pode ser posto contra o mês passado. Semanas
  inteiras a partir do dia 1º, o mês inteiro quando ele fecha. **Zero durante
  toda a primeira semana.**

`comparison` carrega as duas leituras lado a lado (`realized` e
`current`/`previous`), e cada frase leva o sufixo da janela em que foi de fato
medida — no dia 14, "resultado até o dia 13" e "22% abaixo do mês passado (até o
dia 7)" convivem na mesma tela dizendo a verdade cada uma sobre a sua janela.

Do dia 1º ao 7 não existe comparação: `buildTrends` devolve tendências planas,
`hasBaseline` derruba as recomendações, os insights mês a mês não disparam, e
quem consome **tem de dizer que a semana ainda não fechou** — nunca desenhar uma
queda.

O custo é que o número anda de semana em semana e não de dia em dia: entre o dia
8 e o 13 ele fica parado nos dias 1–7. Isso é pagável porque a leitura diária já
existe e já é alinhada por dia da semana: `WeekPace` compara esta semana com a
passada nos mesmos dias. A comparação mês a mês é sobre o mês, e pode esperar
uma semana fechar.

**O runway de caixa conta o recebimento de um dia normal.** Os dias de hoje em
diante são creditados com o que um dia daquele dia da semana costuma receber
(`cashInRates`, mesma janela de 8 semanas do ADR anterior, na base de caixa —
porque runway é dinheiro entrando, e uma venda no crediário não paga uma conta
na sexta), descontado do que aquele dia já tem lançado, para não contar duas
vezes. As **despesas ficam exatamente como agendadas**: elas realmente são
conhecidas com antecedência aqui, e chutar despesas não lançadas amoleceria o
alarme sobre evidência inventada. A projeção fica pessimista, que é a direção
certa para um aviso errar.

`CashPosition.ExpectsReceipts` diz se houve histórico para creditar. Falso — uma
farmácia que nunca registrou entrada — derruba a recomendação, pela mesma razão
que `hasBaseline` derruba as outras: "reduza despesas" não é o conselho de quem
simplesmente ainda não lançou uma venda.

## Consequências

- `SchemaVersion` vai a **5**. Entraram `period.comparableThroughDay` e
  `cashPosition.expectsReceipts`; as tendências e os campos à frente de
  `cashPosition` mudaram de base. O snapshot do dia da subida é recusado pelo
  dashboard-api até o notificador rodar de novo, que é o comportamento definido
  em `snapshot.go`.
- Todo percentual "vs mês passado" agora é sobre `comparableThroughDay`. As
  leituras sobre o próprio mês continuam sobre `throughDay`, e as duas ressalvas
  são métodos separados (`windowSuffix` / `realizedSuffix`) porque uma linha que
  cita a janela errada é uma linha que mente sobre si mesma.
- O digest ganha uma linha na primeira semana — "comparação com o mês passado a
  partir do dia 8" — e os prompts do notificador e do agente exigem que o modelo
  a repita, pela mesma razão do ADR-017: o modelo apaga qualquer ressalva que
  não receba explicitamente.
- Uma leitura a mais por análise: a janela de 8 semanas passa a ser lida nas
  duas bases — transação para a projeção de faturamento, efetiva para o caixa.
  As duas continuam sendo puladas num mês fechado, que não tem dia à frente para
  precificar. Consolidar as leituras de lançamento continua sendo trabalho de
  outro PR.
- A linha de KPIs era o último lugar que ainda lia o `MonthlySummary` cru, e
  carregava a mesma falha por um terceiro caminho: `TotalExpense` e
  `TotalExpectedIn` somam o mês inteiro na base efetiva, então `Despesa` e
  `Resultado` incluíam o aluguel do dia 25 enquanto `Faturamento` e `Entradas de
  Caixa` — que não conseguem conter um dia futuro — só tinham o que já
  aconteceu. Dois cards "até agora" e dois "mês inteiro", lado a lado, com
  `Resultado` subtraindo um tipo do outro. Os dois passam a cobrir os dias que
  já chegaram (`monthClock.elapsed`, que **inclui hoje** — a linha de KPIs é o
  estado do mês agora, não uma medição dele), e o que estava embutido vira
  `DespesaAgendada`, ao lado do número e nunca dentro dele.
- A regra "não mando nada se não houver alerta" continua valendo.
