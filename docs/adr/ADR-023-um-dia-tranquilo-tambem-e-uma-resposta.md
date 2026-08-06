# ADR-023: Um dia tranquilo também é uma resposta

## Status

Accepted

## Contexto

O resumo diário do WhatsApp só saía quando alguma regra disparava: uma conta
vencendo hoje, uma conta vencida, a meta batida. Nos outros dias o notifier
rodava, lia o ledger inteiro, montava a análise do mês — e não mandava nada.

Duas coisas erradas moram nesse silêncio.

**A primeira é que ele é ambíguo.** "Nenhuma conta vence hoje e o caixa está
coberto" e "o cron quebrou" chegam ao celular exatamente da mesma forma: nada.
O usuário não tem como distinguir um do outro, e a única leitura segura é a
pessimista — abrir o painel para conferir. Um resumo diário que precisa ser
conferido não resolveu o problema que existia para resolver.

**A segunda é que o dia tranquilo é justamente a resposta acionável.** A
pergunta que o produto responde é "a farmácia vai conseguir cumprir seus
compromissos?". Um "sim, e não há nada para você fazer hoje" é o melhor
desfecho possível dela, e era o único que nunca era comunicado.

Havia ainda um terceiro problema, do outro lado da mensagem: o modelo recebia
para reescrever apenas as linhas já renderizadas (`DigestLines` e
`AheadLines`). Tudo que a análise sabia mas não imprimia — o saldo projetado,
os compromissos do mês e se estão cobertos, o ritmo da semana — não tinha como
chegar ao leitor, por mais que o dia pedisse. O motor de insights existe desde
o épico #33; o resumo diário lia uma fatia dele.

## Decisão

**O resumo sai todo dia**, dentro da janela de 20h do WhatsApp (sem template
pago — ADR do épico #33), e o opt-out continua sendo o toggle de WhatsApp em
`NotificationPrefs`. Os toggles por tipo de alerta decidem **o que a mensagem
pode afirmar**, não se ela é enviada.

**Um dia tranquilo é dito, não subentendido.** A linha de calmaria
(`calmLine`) abre a metade "a partir de agora" com o que foi verificado —
"nada vence hoje", "não há contas vencidas" — e acrescenta a cobertura do
caixa quando o `CashPosition.Commitments` é `coberto`.

**A afirmação de calmaria vem do ledger, nunca da lista de alertas vazia.**
`notifications.Bills` conta as contas pendentes independentemente de qualquer
preferência, e cada frase só é escrita para quem assinou aquele tipo. Uma lista
de alertas vazia não é prova de que nada vence: quem desligou o alerta de
vencimento não recebe alerta nenhum no dia mais cheio do mês, e escrever "nada
vence hoje" para essa pessoa seria a única frase falsa da mensagem.

**O modelo escreve a partir do JSON de insights inteiro.**
`Analysis.DigestPayload()` é o mesmo payload que o bot recebe, menos as chaves
que só servem a quem pode fazer outra chamada (`secoes_disponiveis`,
`meta_de_outro_dia`) — instruções que um escritor de um turno só não tem como
seguir e pode repetir para o usuário. Ele é derivado de `ToolPayload` de
propósito: um segundo payload montado à mão seria um quarto conjunto de números
para manter de acordo com os outros três.

**Citar é permitido; calcular não.** O prompt autoriza qualquer número que
esteja no JSON ou no rascunho e proíbe soma, subtração e divisão entre eles —
a mesma proibição que o ADR-019 faz à meta por dia, agora dita uma vez para
todas as figuras. É o que torna seguro entregar o payload inteiro: cada número
ali já foi calculado por código com teste atrás.

**O rascunho continua na mensagem do usuário; o JSON viaja no system prompt.**
Não é arbitrário: sem `GEMINI_API_KEY` o `NewDigestGenerator` cai no gerador de
eco, que devolve a mensagem do usuário como está. Manter o rascunho pronto
naquele campo é o que faz uma instalação sem modelo enviar o texto estático em
vez de uma página de JSON.

**As saídas previstas do dia vão em mensagem separada, e inteiras.** O digest
diz o total ("Pagamento de R$ 12.000,00 vence hoje"); a lista item a item sai
logo depois, com cabeçalho próprio, contagem e total. São duas mensagens por
três razões:

1. **O digest é reescrito pelo modelo, e a lista não pode ser.** Uma linha
   somem, dois valores viram "cerca de R$ 3 mil" — a reescrita é exatamente
   onde um pagamento se perde. A lista é a cópia do ledger, entregue verbatim.
2. **Uma lista longa dentro do digest empurra o resto para fora da tela.** O
   resumo é um parágrafo para ler às sete da manhã; a lista é o que se paga a
   partir dela. Juntos, um estraga o outro.
3. **A lista não trunca: ela se parte.** O limite de corpo de texto da Meta é
   4096 caracteres e ela rejeita a mensagem inteira ao passar disso, então um
   dia pesado continua em mensagens seguintes, numeradas ("continuação 2/3"),
   em vez de virar "e mais 4 contas" (ADR-015). Uma única linha maior que o
   orçamento vai longa em vez de cortada: a descrição de uma conta não é nossa
   para encurtar, e uma mensagem rejeitada é um erro mais honesto que um
   pagamento com metade do nome.

A lista segue o mesmo opt-in do alerta que ela detalha (`NotifyDueToday`) — não
é uma porta dos fundos para reportar um tipo que a pessoa desligou. E o prompt
avisa o modelo de que ela vem, só quando vem, para que o digest fique no total
em vez de escrever de memória uma lista que chega completa logo abaixo. As duas
mensagens falham de forma independente: uma lista que não foi entregue é erro
registrado, mas não desfaz o digest nem faz a execução de amanhã repeti-lo.

**Sobra um silêncio, e só um:** quando a análise do mês falha *e* o usuário não
assina nenhum tipo de conta, não há nada verificado para dizer. A mensagem
seria um "resumo do dia" que, pela própria existência, afirma que alguém olhou.
Esse caso tem contador próprio (`SkippedNothingVerified`) e linha de log
própria, pela mesma razão que os outros têm: um dia sem mensagem precisa ser
diagnosticável só pelos logs.

## Consequências

- Duas mensagens por usuário por dia continuam impossíveis (o dedupe por dia
  não mudou), mas o número de mensagens enviadas sobe para ~1/dia/usuário. Com
  2 usuários e a janela de 20h, o custo segue irrelevante.
- O `SkippedNoAlerts` saiu do `Result`. Quem grepava `skipped_no_alerts` no
  CloudWatch passa a grepar `skipped_nothing_verified`, que quer dizer outra
  coisa — bem mais rara.
- O prompt do digest cresceu com o JSON (~1,5 KB por execução, uma vez por
  dia). Irrelevante contra o cap de custo, e é o mesmo dado que o bot já manda
  a cada pergunta aberta.
- `dias_ate_saldo_negativo` deixou de ser declarado como `nil` no payload: em
  um mapa Go a diferença entre "chave ausente" e "chave nula" não aparece, mas
  o digest serializa esse payload em JSON, onde `null` é um valor e se lê como
  "zero dias".
- Fica de fora, por depender do motor de insights e não do resumo: um total de
  **contas a receber** nos próximos dias. O exemplo do épico ("R$ 3.120 a
  receber") só será escrito quando a análise produzir esse número — o resumo
  não vai inventá-lo somando lançamentos.
