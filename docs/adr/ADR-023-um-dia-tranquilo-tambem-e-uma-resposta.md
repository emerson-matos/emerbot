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
pago — ADR do épico #33), para todo destinatário cadastrado.

**E não há opt-in nenhum.** O toggle de WhatsApp e os três toggles por tipo de
alerta foram removidos: a farmácia quer as mensagens, todas, todo dia, e um
interruptor que ninguém ia mexer é um ramo no notifier, um campo no store, um
controle numa página e um jeito de a manhã ficar muda por um motivo que ninguém
lembra ter escolhido. `NotificationPrefs` virou lista de endereços — usuário e
telefone — e a única coisa que ainda impede uma mensagem é não haver número
para onde mandá-la.

**Um dia tranquilo é dito, não subentendido.** A linha de calmaria
(`calmLine`) abre a metade "a partir de agora" com o que foi verificado —
"nada vence hoje", "não há contas vencidas" — e acrescenta a cobertura do
caixa quando o `CashPosition.Commitments` é `coberto`.

**A afirmação de calmaria vem do ledger, nunca da lista de alertas vazia.**
`notifications.Bills` conta as contas pendentes por conta própria. Com os
toggles fora, as duas fontes concordam — mas concordam por como o `Evaluate`
está escrito hoje, e essa é a frase da mensagem que precisa ser verdadeira
independentemente do que façam com aquela função depois. Foi exatamente esse o
buraco enquanto os toggles existiam: quem desligava o alerta de vencimento não
recebia alerta nenhum no dia mais cheio do mês, e "nada vence hoje" teria sido
dito para ele.

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

**E ela sai duas vezes por dia.** Uma lista lida às sete da manhã está esquecida
às três da tarde, e as contas do dia não vencem de manhã — vencem no dia. Então
há uma segunda execução, pouco depois das 15h, que manda **o que ainda está em
aberto**: a lista é remontada do ledger, então uma conta paga às onze não
aparece na de três. É lembrete, não repetição, e quanto mais curta a segunda,
mais ela vale a leitura.

Três consequências dessa segunda execução:

- **Qual execução é ela vem no evento, não no relógio.** O `input` do
  agendamento diz `{"run": "saidas_tarde"}`; o Lambda não deduz nada de
  `time.Now()`. A janela flexível do EventBridge e as retentativas movem a hora
  em que uma execução realmente cai, e um resumo matinal disparado às 15h30
  seria pior que nenhum. Um `run` vazio é o resumo (agendamento antigo continua
  funcionando); um `run` desconhecido é erro, não default.
- **Ela tem chave de dedupe própria** (`…#saidas-tarde`). A do resumo silenciaria
  o lembrete, que é a mensagem que ela existe para não silenciar.
- **Ela não recalcula nada.** Sem análise, sem `MultiMonthlySummary`, sem
  snapshot: a tarde não tem nada de novo a dizer sobre o mês, e um segundo
  snapshot por dia só sobrescreveria o da manhã com os mesmos números.

**A tarde é só sobre os compromissos pendentes do dia, e não tem ramo
silencioso.** Uma pergunta — "o que ainda tenho para pagar hoje?" — e duas
respostas: a lista, ou a linha dizendo que não há nada nela ("nenhum
compromisso pendente"). Um dia em que tudo foi pago e um dia que não tinha
conta nenhuma recebem a mesma frase, porque *pendente* está vazio nos dois e é
disso que a mensagem trata; o que já foi pago não é assunto dela.

Nenhuma das duas é silêncio: um lembrete que não chegou e um lembrete
desnecessário são indistinguíveis do lado de fora, e foi a farmácia mesmo que
pediu a mensagem nos dias vazios.

O prompt avisa o modelo de que a lista vem, só quando vem, para que o digest fique no total
em vez de escrever de memória uma lista que chega completa logo abaixo. As duas
mensagens falham de forma independente: uma lista que não foi entregue é erro
registrado, mas não desfaz o digest nem faz a execução de amanhã repeti-lo.

**Não sobra silêncio nenhum.** Enquanto havia toggles, sobrava um: análise do
mês quebrada *e* usuário sem nenhum tipo de conta assinado deixavam a mensagem
sem nada verificado para dizer. Sem os toggles isso não existe — as contas do
dia são sempre reportáveis, então uma análise quebrada custa à mensagem a
metade retrospectiva e mais nada. A única forma de não receber é não ter
telefone na conta do Cognito, que não é escolha de ninguém e por isso tem
contador (`Unreachable`) e linha de log próprios.

## Consequências

- O volume sobe para 2 ou 3 mensagens por usuário por dia (resumo + lembrete da
  tarde, mais a lista da manhã nos dias com conta a vencer), cada uma com dedupe
  próprio. Com 2 usuários e a janela de
  20h, o custo segue irrelevante — e nenhuma delas pode sair duas vezes.
- O Lambda do notifier passa a ser invocado por dois agendamentos
  (`notifier-daily` e `notifier-open-bills`). O horário da tarde é a variável
  `notifier_open_bills_schedule` (padrão `cron(10 15 * * ? *)`, no fuso de
  `app_timezone`), com janela flexível de 10 minutos em vez dos 30 da manhã —
  meia hora de folga empurraria "pouco depois das 15h" para perto das 16h.
- Os contadores do `Result` mudaram de nome junto com o que descrevem:
  `skipped_no_alerts` e `not_opted_in` saíram, entrou `unreachable`. Toda linha
  de log carrega `run` (`digest` ou `saidas_tarde`): duas execuções por dia sem
  isso viram um log ambíguo.
- **O cadastro de destinatário mudou de dono.** Era o PUT da página de
  preferências que gravava usuário + telefone na tabela que o notifier lê; sem
  formulário, ninguém gravaria nada. Agora quem grava é o `GET
  /notifications/preferences`, quando (e só quando) o que está salvo difere do
  que o Cognito diz. Isso conserta de quebra um telefone trocado na conta, que
  antes ficava desatualizado na tabela até alguém reabrir o formulário e salvar
  — mas passa a depender de alguém abrir a página de Notificações ao menos uma
  vez.
- O PUT saiu; a rota responde 405. A página `/notificacoes` continua existindo,
  sem controles: mostra para qual número as mensagens vão, o que chega todo dia
  e a regra da janela de 20h.
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
