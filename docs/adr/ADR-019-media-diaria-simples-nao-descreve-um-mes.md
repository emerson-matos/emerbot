# ADR-019: Média diária simples não descreve um mês

## Status

Accepted

## Contexto

Em 4 de agosto o bot foi perguntado quanto era preciso vender e respondeu
**R$ 1.200 por dia**. O número não veio de um bug: veio da única conta que os
dados disponíveis permitiam. A ferramenta `get_analysis` devolvia
`falta_para_a_meta_na_projecao` e `dias_restantes_no_mes_com_hoje`, e um modelo
diante desses dois campos e da pergunta "quanto por dia?" divide um pelo outro.

O problema é que **os dias da semana de uma farmácia não faturam igual**. As
médias gaussianas das 8 semanas anteriores, na mesma tela:

| Dia     | Média    |
| ------- | -------- |
| domingo | R$ 600   |
| segunda | R$ 1.224 |
| terça   | R$ 1.191 |
| quarta  | R$ 1.029 |
| quinta  | R$ 1.214 |
| sexta   | R$ 1.211 |
| sábado  | R$ 1.481 |

Sábado vale **2,5× um domingo**. Uma média simples de R$ 1.200 pede de todo
domingo um valor que nenhum domingo da janela alcançou — e pede de sábado menos
do que um sábado ruim já entrega. O alvo é impossível em dois dias de cada sete e
frouxo em outro, e o erro não se cancela: ele aparece exatamente nos dias em que
alguém abriria a loja perguntando "dá para bater hoje?".

Pior: uma farmácia que **não abre** aos domingos recebia, todo domingo, a mesma
cobrança de R$ 1.200 por um dia de porta fechada.

Esse era o último resquício de um cálculo que a projeção do mês já tinha
abandonado. O ADR anterior já havia trocado a média plana pela média gaussiana
por dia da semana em tudo que o painel desenha (`projectionRates`,
`Projection.Remaining`), e o campo `Projection.NeededPerDay` — o "necessário por
dia" aritmético — foi removido do Go. Mas a conta sobreviveu **fora** do código:
o modelo a refazia a cada resposta, porque nada no payload nem no prompt dizia
que ela não vale.

## Decisão

**Nenhum consumidor recebe, calcula ou apresenta uma média diária simples do
mês.** Isso vale para o Go, para o painel e — explicitamente — para o modelo.

A pergunta "quanto preciso vender hoje?" tem uma resposta e só uma:
`Projection.TodayTarget`, a fatia de hoje no que falta, escalada pelo ritmo do
próprio dia da semana (`historical × factor`). Um domingo pede o que um domingo
pode dar.

Três mudanças sustentam isso:

1. **`meta_de_hoje` no payload da ferramenta.** O número existia desde o card
   "Meta para hoje", mas era exclusivo do painel — o `ToolPayload` não o
   carregava, então o bot literalmente não tinha acesso à média gaussiana. Agora
   vem junto de `media_historica`, porque "R$ 1.480" só significa alguma coisa ao
   lado do "um sábado costuma dar R$ 1.481".

2. **`TodayTarget.State` em vez de `Valid bool`.** Quando não há meta do dia, o
   motivo é dito: `meta_batida`, `dia_sem_movimento`, `sem_historico`,
   `sem_meta`, `mes_fechado`. Um booleano colapsava seis situações — inclusive a
   boa notícia "a meta do mês já foi batida" — no mesmo silêncio, e silêncio é
   justamente o que fazia o modelo preencher a lacuna com a divisão.

3. **Proibição explícita no prompt do agente.** "NUNCA divida o que falta para a
   meta pelo número de dias restantes, e nunca calcule você mesmo uma média
   diária do mês." Uma regra negativa sozinha não basta — um modelo sem número
   para dar inventa um —, por isso ela vem sempre acompanhada do campo que
   responde à pergunta.

O digest diário do WhatsApp passa a abrir com a mesma linha ("Meta de hoje
(domingo): R$ 600 — em linha com o que um domingo costuma faturar"), pelo mesmo
motivo: o rascunho não carregava número diário nenhum, e o humanizador do
notificador preenchia o vazio com a conta que este ADR proíbe.

## Consequências

- `Projection` não tem, e não deve voltar a ter, um campo de valor por dia que
  não seja por dia da semana. Os últimos comentários que citavam `NeededPerDay`
  foram removidos, junto do campo fantasma `neededPerDay` que o TypeScript ainda
  declarava para um JSON que o Go não emitia mais.
- Uma média aritmética continua legítima **para descrever o passado** — "o mês
  fechou com R$ X por dia" é um fato. O que este ADR proíbe é usá-la como
  **alvo**: descrever é dividir o que aconteceu, projetar é distribuir o que
  falta, e só a segunda precisa saber que dia da semana é amanhã.
- O painel e o bot passam a ler o mesmo número pela mesma razão, o que fecha o
  último caminho pelo qual as duas telas divergiam sobre o mesmo mês.
- Fica registrado o limite do modelo: **dar acesso a um dado não é o mesmo que
  impedir uma conta errada.** As duas coisas precisam andar juntas — o campo e a
  proibição —, e é por isso que a decisão está em um ADR e não só em um commit de
  prompt.
