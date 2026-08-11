# ADR-027: O caderninho é à parte — fiado não vence, envelhece

## Status

Accepted

## Contexto

A farmácia vende fiado e quer passar a registrar. Não há nenhum fiado no razão
hoje, então isto é um começo do zero.

O que se pediu é **controle interno**: um caderninho. Quem deve, quanto, desde
quando. Não é uma extensão do sistema financeiro, e **não deve alterar nenhuma
métrica existente**.

Isso é uma decisão de produto, não uma omissão, e ela está aqui escrita porque a
leitura contrária é a intuitiva: a ADR-016 diz que uma venda conta como
faturamento no dia em que é feita, paga ou não. Um desenho que seguisse essa
linha colocaria a venda fiado no razão — e aí ela cairia em `EffectiveDate` (sem
vencimento, na data da venda) e entraria em sete leitores que somam dinheiro sem
olhar `PaymentStatus`: `TotalExpectedIn`, `KPIs.Resultado`, `Trends`, os "dias no
azul" do `Health`, o `total_entradas` das listagens, a curva de caixa, e o
`cashInRates` que alimenta o veredito `coberto`/`descoberto` da ADR-022. A venda
fiado seria contada como dinheiro que entrou, no ato da venda.

O caderninho à parte não tem esse problema porque não encosta em nada disso. É a
razão pela qual o desenho abaixo é pequeno.

(O fato de esses sete leitores somarem pendente por `EffectiveDate` é um bug que
já existe hoje para qualquer conta a receber com vencimento. Ele fica registrado
aqui porque foi descoberto neste trabalho, mas **não** é pré-requisito deste
caderninho e não é resolvido por ele.)

O fato de negócio que decide o resto veio do dono: **quando ele vende fiado, ele
não sabe quando vai receber.** Não há data combinada, não há parcela acordada,
não há carnê. O cliente paga aos poucos e quando dá.

## Decisão

### 1. O caderninho não toca no razão

Não há `FinancialEntry` de venda fiado. Não há campo novo em `FinancialEntry`.
Nenhum somatório, nenhuma métrica, nenhum snapshot, nenhum índice e nenhum
recurso de infraestrutura muda.

**Venda fiado não é faturamento**, não é caixa e não é previsto. Faturamento
continua sendo o que os lançamentos atuais dizem que é. O caderninho responde uma
pergunta só — quem me deve — e não participa de nenhuma outra.

Dar baixa no caderninho também **não** cria lançamento de caixa. Se o dinheiro
que o cliente pagou tem que aparecer no razão, ele é lançado do jeito que já se
lança qualquer entrada hoje. Os dois sistemas são independentes de propósito.

### 2. Fiado não vence, envelhece

Nada foi prometido, então nada está atrasado. O vocabulário é obrigatório em toda
superfície — Go, dashboard, prompt: **"em aberto há N dias"**, contado da data em
que a dívida começou. Nunca "vencido", nunca "atrasado", nunca "inadimplente".

Pela ADR-017, uma dívida de hoje não tem idade medível: o aging começa em N ≥ 1.

### 3. O movimento é a verdade, o latest é a foto

Três tipos de item na partição do próprio usuário:

```
PK = USER#<id>
SK = FIADO#<cliente_slug>                    → o latest
SK = FIADODIA#<data>#<cliente_slug>#<ulid>   → um movimento, na ordem do dia
SK = FIADOCLI#<cliente_slug>#<data>#<ulid>   → o mesmo movimento, na ordem do cliente
```

O **movimento** é o registro do que aconteceu: valor, data e descrição. Ele é a
verdade — o saldo de um cliente **é** a soma dos movimentos dele.

O **latest** é a foto depois do último movimento: nome como o usuário digitou,
saldo e `desde`. Ele existe porque "quanto o fulano me deve agora" é a leitura mais
quente do caderninho e a tela que o front abre primeiro — e uma soma de histórico
não é resposta para isso. É cache, não fonte.

**O sinal do valor é o tipo.** Positivo é dívida, negativo é pagamento — não há
campo `tipo`, não há enum, não há dois contadores a manter. `ADD Saldo :n` aceita
negativo nativamente, então compra e pagamento são a mesma escrita com o sinal
trocado, e "quanto o João me pagou" é a soma dos negativos do histórico dele.

**As três chaves são irmãs, não aninhadas.** A forma óbvia seria pendurar os
movimentos debaixo do cliente (`FIADO#joao#…`), e ela quebra a consulta mais
frequente: `begins_with(SK, "FIADO#")` passaria a arrastar o histórico inteiro de
todos os clientes só para listar quem deve, e uma key condition não filtra por
sufixo. Com prefixos distintos nenhum casa com o outro — depois de `FIADO` vem
`D`, `C` ou `#` — e cada pergunta custa uma leitura:

| Pergunta | Custo |
|---|---|
| "como estão meus fiados" | uma Query em `FIADO#`, só os latests |
| "como foram meus fiados no dia X" | uma Query em `FIADODIA#<data>#` |
| "como foram meus fiados para o João" | uma Query em `FIADOCLI#joao#` |
| "quanto o João me deve" | `GetItem` |
| "quanto o João me pagou" | soma dos negativos da Query acima |
| "desde quando o João me deve" | `GetItem` |

O `<ulid>` no fim não é decoração: sem ele, dois movimentos do mesmo cliente no
mesmo dia — rotina no balcão — se sobrescreveriam em silêncio.

**Por que o mesmo movimento é gravado duas vezes.** "No dia X" e "para o João"
querem a mesma linha em duas ordens, e uma chave só serve uma delas: a outra
viraria leitura do histórico inteiro com filtro em Go. A alternativa canônica
seria um GSI, e ele foi descartado pelos motivos da seção 4 — a duplicata custa
uma escrita pequena a mais por movimento e mantém tudo fortemente consistente,
sem tocar no orçamento de capacidade. As duas cópias vão na mesma transação do
`ADD` no latest, então não existe estado em que só uma delas exista.

**O cache é mantido por contador atômico.** `ADD Saldo :n` no `UpdateExpression`,
com o sinal do movimento, na mesma transação que grava as duas cópias. Não lê
antes, logo não há janela de lost update: dois pagamentos em paralelo, ou dois
retries do Lambda, somam certo.

E porque é cache, **ele é reconstruível**: somar `FIADOCLI#<cliente>#` devolve o
saldo daquele cliente, sempre. Recalcular não é perda de dado, é o conserto — o
oposto do que seria se o saldo fosse declarado.

Guardar `total_pago` e `total_comprado` no latest foi considerado — daria a
resposta num `GetItem` em vez de numa Query — e descartado: são dois contadores a
mais para manter em sincronia, e a Query que os substitui é a mesma que responde
"como foram meus fiados para o João".

`desde` significa **"está devendo sem parar desde X"** — a data em que o saldo
saiu de zero, limpa quando volta a zero. Não é "a compra mais antiga em aberto":
as duas divergem quando o cliente zera e volta a comprar, e esta é a que responde
"há quanto tempo esse cliente me deve" sem ler um movimento sequer, o que é o que
faz o caderninho inteiro caber numa Query.

**O saldo é por pessoa, e um pagamento não abate compra nenhuma.** O que o dono
digita é *"o joão me pagou 50"*: ele não nomeia compra, e frequentemente não há uma
a nomear — se o João tem uma compra de 30 e outra de 40 em aberto, 50 não fecha
nenhuma das duas. Alocar seria trabalho do LLM, em silêncio, e um caderninho errado
sem nada capaz de perceber. Os movimentos dizem o que aconteceu; o saldo diz quanto
falta; nada diz qual compra específica continua descoberta, porque essa pergunta
não é feita.

**O movimento é para ser visto, não só somado.** As duas ordens são as duas
timelines que o usuário lê: `FIADODIA#` é o caderninho em ordem cronológica,
`FIADOCLI#joao#` é o extrato de uma pessoa. A Query devolve nessa ordem sem
ordenação em memória, `ScanIndexForward=false` põe o mais recente primeiro, e o
ULID desempata de forma estável dois movimentos do mesmo dia.

A lista pagina de verdade: `Limit` mais `ExclusiveStartKey` do próprio DynamoDB,
e pela ADR-015 uma lista cortada nunca sai calada — quem corta avisa que cortou.

**Não há saldo por linha.** A timeline mostra os movimentos, e o saldo aparece uma
vez, vindo do latest. Calcular o saldo acumulado a cada linha é possível
(desandando a partir do latest) e foi descartado: é coluna que ninguém pediu, e
que numa lista paginada só faz sentido se a página começar do fim.

O sinal também deixa de ser convenção interna e vira o que a pessoa lê: `+R$
40,00` é o que ela levou, `−R$ 50,00` é o que ela pagou.

**A dívida é sempre agora.** Não existe "quanto o João me devia em julho" — a
pergunta é sempre quanto ele deve hoje. Poder responder o passado seria consequência
gratuita de o saldo sair dos movimentos, não requisito; o latest não guarda
histórico de saldo e ninguém deve construir um.

**Corrigir é apagar o movimento errado**, não postar linha compensatória: as duas
cópias somem e o inverso vai no cache, numa transação só — e depois se lança o
movimento certo, se havia um. É o que se faz num caderninho de papel, e é o que
mantém o sinal como único discriminador: um ajuste postado como linha entraria em
"quanto o João me pagou" contando dinheiro que nunca existiu, e evitar isso custaria
um campo de tipo que este desenho não tem.

O preço é que uma correção não deixa rastro — o movimento errado some como se nunca
tivesse sido digitado. É a mesma escolha da timeline sem auditoria, e é coerente
com ela.

### 4. Na partição base, não num índice

Três motivos, em ordem de peso:

1. **Consistência forte.** Um GSI no DynamoDB nunca aceita leitura consistente.
   "fiado 40 do joão" seguido de "quanto o joão me deve?" chega em segundos, e a
   segunda mensagem leria um índice sem a primeira escrita — o bot responderia um
   número errado com toda a confiança. A ADR-014 registra que o `dynamotest` não
   modela consistência eventual em GSI, então isso seria verde na suíte e errado
   em produção.
2. **Custo zero.** As tabelas são `PROVISIONED` de propósito para caber nos 25/25
   do Always-Free (ADR-008); o orçamento em `infra/modules/api_gateway_lambda`
   soma 22/18 e sobram 3 RCU / 7 WCU **para a conta inteira**. Um GSI custaria
   dois de cada. Isto não custa nada: nem atributo declarado, nem diff de Tofu.
3. As seis consultas da seção 3 são `GetItem` e Queries com `begins_with`. O
   repositório já usa vizinhos na partição (`CAT#`, `ENTRY#`, prefs) e o
   `dynamotest` já modela `begins_with` em key condition, sem uma linha nova.
   Um GSI serviria a segunda ordem do movimento com uma escrita em vez de duas,
   mas custaria capacidade e leitura eventual num lugar onde a duplicata custa
   uma escrita pequena e nada mais.

### 5. O nome do cliente se reconcilia antes de gravar

Sem isso o caderninho não funciona. Ninguém digita nome de forma consistente:
"joão", "João Silva" e "Joao S." normalizam para `joao`, `joao_silva` e `joao_s` —
três devedores onde há um. E o caderninho mentir **para menos** ("o João me deve
40" quando são 340) é o que faz o dono parar de usar.

A solução não é um cadastro de clientes com CRUD. É a jogada da ADR-024 aplicada a
pessoas: **o agente consulta antes de criar.** Se o slug digitado não existe mas há
parecido, ele **pergunta** ("É o João Silva, que já tem R$ 300 em aberto, ou outro
João?"). O cadastro emerge do caderninho em vez de ser mantido — a consulta é uma
Query barata e fortemente consistente na mesma partição.

`NormalizeCategorySlug` é reusada: uma forma de slug só no sistema.

**Fiado sem cliente é recusado.** "Fiado de quem?" é uma pergunta de uma palavra, e
uma dívida anônima é irrecuperável.

### 6. Tools próprias, e nenhuma existente muda

O caderninho ganha suas ferramentas — registrar compra, dar baixa, consultar um
devedor, listar o caderninho. `create_financial_entry` e `edit_financial_entry`
não são tocadas, `recebimento_cliente` continua fora do enum de origens
(`createOriginSlugs`), e `list_due_entries` não tem nada a ver com fiado: ela é
sobre vencimento, e fiado não tem.

O prompt ganha a seção do caderninho e uma regra dura: fiado nunca vira
lançamento, e lançamento nunca vira fiado.

## Consequências

- **O faturamento fica menor que a venda real enquanto a dívida está aberta.**
  Decisão consciente, e o efeito cresce com quanto se vende fiado: meta, projeção,
  comparação com o mês passado e a quebra por categoria não sabem dessas vendas.
  Reverter isso não é mexer no caderninho — é a decisão oposta à da seção 1, e
  passa pelo predicado que a manteria fora de caixa e de previsto.
- **Nada reconcilia o caderninho com o razão**, de propósito: são dois sistemas.
  Dentro do caderninho, latest e movimentos são escritos na mesma transação e o
  latest é sempre a soma dos movimentos — se divergirem, é bug, e o conserto é
  recalcular (seção 3).
- **Os movimentos crescem para sempre, e agora não dá para simplesmente expirá-los.**
  São dois itens pequenos por movimento, num volume de balcão, então não incomoda —
  mas como o saldo sai deles, um TTL apagaria dívida. Se um dia precisar, é snapshot
  mais poda, não TTL solto.
- **Nada existente muda**: nem `SchemaVersion`, nem o notifier, nem
  `packages/notifications` e seu gêmeo em `apps/web/src/lib/notifications.ts`,
  nem o Tofu, nem a configuração do `dynamotest`. O PR é aditivo.
- **Saldo negativo é possível** e significa crédito do cliente (pagou mais do que
  devia). Registrar é melhor que recusar: o movimento aconteceu de verdade.
- **Calote e devolução não são casos especiais**: reduzem o saldo do devedor como
  qualquer baixa, e nenhum deles toca no razão — pela seção 1, não há faturamento a
  estornar. O que falta é vocabulário: com o sinal como único discriminador (seção
  3), os dois entram como negativo e ficam indistinguíveis de um pagamento — o
  dinheiro que nunca chegou soma junto com o que chegou. Se essa diferença passar a
  importar, é aí que um campo de tipo entra, sem mudar chave nem modelo. Está
  escrito para que o modelo não invente uma forma quando o dono disser "o joão
  devolveu o remédio".
- **Se o caderninho entrar no digest**, ele é mensagem própria com chave de dedupe
  própria (ADR-023), e pela ADR-022 o total em aberto não vai sozinho: um número
  que pode ser lido como veredito vai com o veredito. **A partir de quantos dias
  um fiado vira assunto é decisão em aberto** — sem vencimento, o limiar é régua de
  negócio e não sai de nenhum dado.
