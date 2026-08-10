# ADR-025: A meta do dia nunca é menor que a média do dia

## Status

Accepted

Refina o ADR-019 e o ADR-021. Não muda a forma da distribuição — cada dia
continua sendo cobrado pelo ritmo do seu próprio dia da semana —, muda o piso
dela.

## Contexto

O ADR-019 tirou a média diária simples e pôs no lugar uma distribuição: o que
falta para a meta do mês repartido entre os dias restantes, cada um na proporção
do que aquele dia da semana costuma faturar. A propriedade que fazia dela um
plano estava escrita no tipo `Plan`:

    Σ média[dia_da_semana(d)] × Factor, para d em hoje..fim  ==  o que falta

Ela fecha o mês. E fecha o mês **exatamente**, que é o problema: quando o mês
está adiantado, `Factor` cai abaixo de 1 e a conta pede de cada dia **menos do
que aquele dia da semana já costuma vender**.

Não é um caso raro nem um caso de borda. Uma farmácia que vinha bem no dia 27 de
julho, com R$ 2.500,00 faltando e cinco dias que valem R$ 5.000,00 no ritmo
normal, recebia no digest das sete da manhã:

> Meta de hoje (segunda): R$ 500,00 — 50% abaixo do que uma segunda costuma
> faturar (R$ 1.000,00), o mês está adiantado.

O número está certo em relação à meta e errado em relação a tudo o mais. Uma
meta de R$ 500,00 numa segunda que faz R$ 1.000,00 não é uma meta: é uma licença
para vender metade. O painel dizia a mesma coisa em ouro, com o traço tracejado
abaixo da barra da própria segunda-feira, e a régua da semana — que existe para
mostrar o ritmo — ficava por cima da cobrança.

No extremo, a incoerência virava descontinuidade. Com a meta do mês batida,
`missing <= 0` e o plano devolvia `meta_batida`, um estado **sem meta nenhuma**.
Então:

- a 99% do ritmo, o dia era cobrado por quase a média;
- a 101%, o dia não era cobrado por nada, por onze dias seguidos se a meta caiu
  no dia 20.

E `meta_batida` chegava ao leitor pelo mesmo caminho de "não há histórico": um
espaço em branco no card e um `situacao` diferente de `ok` no payload. O
ADR-021 já tinha nomeado esse problema para os outros casos — "não há meta" e "a
meta é de um mês que não vejo daqui" são respostas diferentes — mas resolveu-o
nomeando as ausências, e esta em particular não devia ser uma ausência.

Havia ainda um terceiro fio solto, do mesmo nó. `Projection.Projected` responde
"onde o mês fecha se os dias seguirem o ritmo de sempre" e sempre respondeu bem.
Ninguém respondia "e se eu bater a meta todo dia?". A única resposta disponível
era a meta do mês — que, numa farmácia adiantada, é **menor** que a projeção
passiva. O prêmio por bater a meta todos os dias era um mês pior do que não
fazer nada de diferente.

## Decisão

**A meta de um dia é o maior entre a fatia do que falta e a média histórica
daquele dia da semana.** O piso é o próprio ritmo da farmácia.

    GapFactor = o que falta / Σ média[dia_da_semana(d)]
    Factor    = max(1, GapFactor)
    meta(d)   = média[dia_da_semana(d)] × Factor  ≥  média[dia_da_semana(d)]

A invariante do `Plan` muda junto, e a nova é a decisão inteira em uma linha:

    Σ meta(d)  ==  max(o que falta, Σ média[dia_da_semana(d)])

O plano pede o que falta **ou** o ritmo de sempre, o que for maior. O que ele
mantém do ADR-019 é a forma da distribuição: cada dia no seu próprio ritmo,
nunca uma média diária achatada que pede de um domingo de R$ 600,00 os
R$ 1.200,00 de um sábado.

Três consequências, todas deliberadas:

**1. Meta batida deixa de ser ausência.** `DayTargetGoalMet` não é mais
produzido: o dia volta `ok`, cobrado pelo ritmo dele. A boa notícia continua
sendo dita — ela só mudou de campo. Vai em `DayTargetSource`, que diz de que a
meta é feita:

- `plano` — a fatia do que falta, acima da média: o mês está atrás;
- `media` — o piso, com a meta do mês ainda em aberto: mantenha o ritmo;
- `meta_batida` — o piso, com a meta do mês já alcançada: você chegou lá.

O campo existe porque os números pararam de distinguir esses casos. Depois do
piso, uma quarta cobrada em exatamente R$ 1.000,00 é um mês em dia e um mês já
resolvido, e "a meta de hoje é R$ 1.000,00" se lê igual nos dois. É a mesma
regra que o `DayBasis` do ADR-021 aplica no outro eixo: quando dois estados
produzem os mesmos números, o estado vai junto.

**2. `esforco: "below"` some para dias à frente.** A meta nunca fica abaixo da
média, então `meta_vs_media_pct` nunca é negativo. `PaceBelow` continua vivo,
mas só em `desempenho` — o que um dia **fechado** de fato vendeu contra o que
aquele dia da semana costuma dar. Continuam sendo eixos opostos, como o ADR-021
já dizia: uma cobrança acima da média é um mês atrasado, um realizado acima da
média é um bom dia.

**3. Existe uma segunda projeção de fechamento.** `Projection.PlannedClose` é
onde o mês fecha se cada dia vender o que a meta dele pede, ao lado de
`Projected`, que é onde ele fecha se os dias apenas seguirem o ritmo. Com o piso
no lugar:

    PlannedClose  ==  max(Projected, meta do mês)

Bater a meta todos os dias nunca fecha o mês pior do que não mudar nada. Ela
soma as metas arredondadas dia a dia, e não a média total vezes o fator, para
que o fechamento do mês e as metas que `get_meta_do_dia` devolve não possam
divergir por arredondamento.

O que **não** muda: `Projected`, `Remaining` e a curva de caixa continuam sendo
previsão pura pela média. A meta com piso não realimenta a projeção — se
realimentasse, "o que esperamos" viraria "o que pedimos", e a única projeção
falsificável do painel deixaria de ser falsificável.

E `sem_meta` continua sendo ausência: meta de dia é fatia de meta de mês, e sem
meta de mês não há nada sendo perseguido. A média daquele dia da semana continua
disponível em `media_historica` e em `media_por_dia_da_semana`, nomeada pelo que
é.

## Consequências

- `SchemaVersion` vai a 11. Não é pelos campos novos (`plannedClose`,
  `plan.gapFactor`, `dayTarget.source`): é porque `todayTarget.target` muda de
  significado. Um snapshot v10 lido nesta versão tem metas abaixo da média e
  `source` vazio, e o painel serve o snapshot guardado — na manhã seguinte a um
  deploy ele desenharia as cobranças de ontem com as palavras de hoje ("o
  esperado para o dia") sobre um número que não é o esperado para o dia.
- A ordem dos guards de `newPlan` muda: "sem histórico" passa a ser verificado
  antes de "meta batida". O piso *é* a média, então um razão sem nada na janela
  não tem o que cobrar de qualquer jeito — e a ordem antiga fazia um razão sem
  vendas com meta batida reportar `meta_batida` sobre médias que não existiam.
- O prompt do agente ganha a regra explícita ("nunca sugira meta abaixo da
  média") e a leitura das três origens. A linha que descrevia `meta_batida` como
  `situacao` sai: ela não chega mais por ali.
- O digest perde a frase "X% abaixo do que uma segunda costuma faturar, o mês
  está adiantado". Não há substituta com o mesmo sentido, e é isso que se
  pretende: o mês estar adiantado não é motivo para pedir menos.
