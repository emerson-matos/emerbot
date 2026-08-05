# ADR-021: Uma meta de dia tem regime, e o dia é um parâmetro

## Status

Accepted

Corrige a forma do ADR-020, não a decisão dele.

## Contexto

O ADR-020 resolveu "e amanhã?" criando `meta_de_amanha` no payload. Funcionou, e
por isso mesmo escondeu o problema: **a pergunta seguinte é "e no sábado?".**
Depois dela vem "e no dia 15?", e "como foi o dia 12?". Um campo por dia é a
mesma resposta errada com um dia a mais em cima. Quem lê `meta_de_hoje` e
`meta_de_amanha` lado a lado no `ToolPayload` vê um padrão que só cresce.

Havia um segundo problema, mais silencioso, que só apareceu quando o dia virou
uma pergunta aberta: **os três números de uma meta de dia — meta, média
histórica, diferença — se leem exatamente igual antes e depois do dia
acontecer.** "R$ 1.052,00 na quarta" é uma aposta se a quarta ainda não chegou e
um fato se ela já passou, e nada no payload dizia qual. A linha existia no
código desde o ADR-017 ("o que é medido termina ontem, o que é cobrado começa
hoje"), mas era implícita: estava em *qual campo* o consumidor calhava de ler.

E havia um terceiro, no painel, do mesmo tipo de todos os anteriores — dois
consumidores da mesma análise recebendo recortes diferentes:

O gráfico "Fluxo de Caixa do Mês" desenhava `RunningBalance`, a curva **só
lançada**, e chamava o trecho tracejado de "projeção". É exatamente a curva que
`buildCashPosition` documenta como "honesta sobre o passado e sistematicamente
errada sobre o futuro": as contas do mês são lançadas no dia 1º e as vendas
registradas conforme acontecem, então o rabo dela mergulha todo mês. Ao lado, o
card lia `summary.ExpectedBalance` — o mês inteiro de despesas contra o esperado
a receber, a conta que o ADR-017 chamou de negativa por construção e que
`KPIs.Resultado` já tinha abandonado. Ela sobreviveu porque o `Dashboard.tsx` lia
o summary direto, sem passar pela análise.

Resultado: a página Análise dizia "projeção fim do mês: +R$ 12.497,16", o bot
dizia o mesmo, e o painel desenhava um colapso e imprimia um terceiro número.

## Decisão

**O dia é um parâmetro, e toda resposta sobre um dia diz em que regime ela
está.**

### 1. `Plan` publicado, `DayTargetOn` para qualquer data

O fator único que distribui o gap (ADR-019/020) deixa de ser privado do build e
vira `Projection.Plan`. Com ele, `Analysis.DayTargetOn(data, realizado, now)`
precifica qualquer dia **a partir da análise já montada** — o plano, as médias
por dia da semana e o relógio saem todos do que já foi calculado. Um dia
precificado assim não tem como divergir do `meta_de_hoje` ao lado dele, e um
snapshot guardado também sabe responder.

`Plan` é deliberadamente **não** derivado de `TodayTarget`. Os dois divergem num
caso que não é raro: num domingo em que a farmácia não abre, `TodayTarget` é
`dia_sem_movimento` sem fator nenhum, enquanto o plano por trás dele está
perfeito e continua precificando os outros seis dias. Ler o plano a partir de
hoje espalharia a porta fechada de hoje pelo mês inteiro.

`meta_de_hoje` **fica** no payload: o ADR-019 exige que a meta do dia chegue sem
ser pedida, ou o modelo deriva uma. Todo o resto é a ferramenta
`get_meta_do_dia(datas[])`, e o payload passa a nomeá-la em `meta_de_outro_dia`
— o mesmo padrão de `secoes_disponiveis`: dizer o que mais é alcançável em vez
de deixar o modelo concluir que não existe.

### 2. `apuracao`: realizado, em curso, projetado

| regime | quando | o que carrega |
| --- | --- | --- |
| `realizado` | dia fechado | o que vendeu + a média daquele dia da semana. **Sem meta** |
| `em_curso` | hoje | meta + média + o que já entrou |
| `projetado` | dia à frente | meta + média |

O `realizado` sem meta é a parte que exige defesa. Seria fácil aplicar o fator de
hoje a um dia passado e devolver um número — e seria **uma meta inventada para
trás**: o plano distribui o que falta *agora*, e o dia que ele estaria cobrando
é um em que ninguém pode mais vender. Reconstruir a meta que aquele dia
realmente teve exigiria remontar a análise na manhã dele. Um dia fechado é
relatado, não cobrado; é a linha do ADR-017 virando um campo.

`Delta`, `DeltaPercent` e `Status` comparam contra a média histórica **o número
que o regime carrega**: a meta num dia à frente, o realizado num dia fechado. É
a mesma pergunta ("esse foi um dia pesado ou leve para essa quarta?") e uma só
dupla de campos para ela.

Dois estados novos, ambos porque `monthClock` não sabe distinguir sozinho:
`mes_futuro` (um mês que não começou reporta `inProgress: false` igual a um
fechado, então "3 de setembro" perguntado em agosto respondia `mes_fechado` — o
oposto da verdade) e `dia_fechado`.

### 3. A curva projetada sai do backend

`CashPosition.Forecast` guarda a série inteira, montada no laço que
`buildCashPosition` **já percorria** e descartava. Os escalares que já existiam
(`EndOfMonthProjection`, `LowestProjected`, `NextDay`) passam a ser leituras
dela, não uma segunda passada.

O gráfico do painel lê essa série. Antes de hoje nada é creditado, então a mesma
série carrega a metade realizada e a projetada — uma linha sólida e uma
tracejada, e a legenda passa a dizer o que a tracejada é. **O navegador não
recalcula nada**: foi assim que a projeção acabou computada duas vezes e
diferente, e é o erro que o ADR-019 mais insiste em não repetir.

`Forecast` fica **fora** do `ToolPayload`. Trinta e um dias de série custam
tokens em toda pergunta de chat, e o bot precisa de um dia, não de uma curva.

No painel, "Saldo Previsto" vira "Projeção Fim do Mês"
(`cashPosition.endOfMonthProjection`, com a ressalva de `expectsReceipts`), e
entra "Faturamento Projetado" (`projection.projected` e `coverage`, com a
ressalva de `basis`). A linha de KPIs não cresce por isso: o slot já existia,
ocupado por um número que o ADR-017 tinha condenado.

## Consequências

- `SchemaVersion` vai a **9**: `projection.nextDayTarget` e
  `projection.todayRevenue` saíram, `dayTarget` ganhou `basis` e `realized`,
  `projection` ganhou `plan`, `cashPosition` ganhou `forecast`.
- `get_meta_do_dia` aceita até 7 datas e avisa quando corta (ADR-015). Cada mês
  distinto por trás delas custa um `Assemble`, e é por isso que o limite existe.
  O `realizado` de cada dia sai de uma leitura do mês na base de transação — não
  de uma série diária dentro de `Analysis`, que colocaria trinta e um números a
  mais em todo snapshot para servir o dia que alguém perguntou.
- O endpoint `/summary/cashflow` continua de pé; o painel só deixou de usá-lo.
  Ele devolve a curva lançada, que é um dado legítimo — o que não era legítimo
  era desenhá-la com a legenda de outra coisa.
- Fica registrado o padrão que se repete desde o ADR-016: **quando dois
  consumidores da mesma análise divergem, quase sempre um deles está lendo um
  recorte mais antigo.** O painel lia o summary, o gráfico lia a curva lançada, e
  o bot lia a análise. A correção nunca é reconciliar os números na ponta; é
  fazer todo mundo ler o mesmo cálculo.
- E o limite que o ADR-020 não viu: **resolver a pergunta que chegou não é o
  mesmo que resolver a forma dela.** "E amanhã?" tinha uma resposta boa e uma
  forma ruim, e a forma ruim só aparece na pergunta seguinte.
