# ADR-024: Uma categoria é uma só, e o faturamento se quebra por ela

## Status

Accepted

## Contexto

O agente do WhatsApp não podia criar categorias, e o faturamento era um número
só. As duas coisas são a mesma lacuna vista de dois lados.

**No lançamento.** O schema das tools trazia `category` como um `enum` com os 18
slugs de `domain.DefaultCategories`. O schema é montado uma vez, na subida do
processo, e as categorias são por usuário — então o enum só podia listar os
padrões. Uma categoria criada no dashboard era invisível para o bot, e um slug
fora do enum caía num `knownCategory()` que trocava a categoria por
`outros_despesas`/`outros_receitas` **em silêncio**, respondendo "lançado".

Isso era defensável enquanto o conjunto era fechado: o que não estava no enum só
podia ser alucinação, e alucinação em "Outros" não perde nada. Deixa de ser no
instante em que a categoria passa a ser do usuário — aí o mesmo slug tem tanta
chance de ser invenção quanto de ser a `venda_varejo` que ele pediu na mensagem
anterior.

**Na apresentação.** A farmácia vende no balcão e vende no atacado, e o sistema
respondia as duas com um número só. A análise tinha `expenseComposition` — a
despesa quebrada por categoria, no dashboard e no payload do bot — e nada
equivalente do lado da entrada. Perguntado "vendi mais no atacado ou no
balcão?", o modelo não tinha o dado: ou ficava calado, ou somava os lançamentos
à mão (o que a ADR-015 proíbe, porque a lista pode vir cortada), ou alcançava
`maiores_despesas`, que é o dinheiro indo para o outro lado.

Havia ainda o risco que a ADR-016 documenta: a regra de faturamento já foi, um
dia, "categoria diferente de `outros_receitas`", e **qualquer categoria de
entrada criada pelo usuário entrava na meta**. Deixar o usuário criar categorias
sem cuidado seria reabrir exatamente esse buraco.

## Decisão

### 1. Uma categoria só, um padrão só

O agente ganha `create_category` e `list_categories`, e **tudo passa por
`domain.NewCategory`** — o dashboard também, que antes gravava o slug cru do
corpo da requisição.

O padrão é o das categorias que já existem: slug em `snake_case` ASCII derivado
do label, acentos rebaixados (`Convênio Farmácia` → `convenio_farmacia`),
pontuação descartada, tamanho limitado. `NormalizeCategorySlug` é a única
definição disso. Um slug proposto por quem chama é sugestão, não chave: passa
pela mesma normalização. Assim "Venda Atacado", "venda-atacado" e "vendaAtacado"
são a mesma categoria, e nenhum consumidor precisa saber que houve três
grafias.

**Não existe "categoria de cliente" como classe.** O campo `Category.Default`
não diz "é do sistema" — diz **onde a definição mora**: `true` é o que está
escrito em `DefaultCategories` no code base e é reconciliado em todo usuário
(`ReconcileDefaultCategories`); `false` é o que este usuário pediu. Fora isso,
uma categoria criada no WhatsApp é igual às outras em todo lugar: no dashboard,
nas tools, nas quebras.

### 2. Categoria desconhecida recusa o lançamento

A coerção silenciosa para "Outros" morreu. `create_financial_entry` e
`edit_financial_entry` validam a categoria contra o catálogo do usuário — os
padrões **mais** o que ele criou — e recusam o que não existe, devolvendo ao
modelo a lista de slugs válidos e o nome da tool que cria uma nova. O erro volta
para o modelo como resultado da tool (o loop continua), então ele corrige e
tenta de novo no mesmo turno.

O tipo também tem que bater: receita não entra em categoria de despesa. Uma
venda arquivada em `aluguel` é uma quebra que mente depois.

Os padrões entram no catálogo mesmo sem estarem gravados: eles são semeados
preguiçosamente no primeiro `GET /categories`, e um usuário que só falou com o
bot tem a partição `CAT#` vazia — validar só contra o store recusaria "aluguel"
para ele.

### 3. Faturamento se quebra por categoria, e origem continua decidindo

`Analysis.RevenueComposition` é a nova quebra, e ela aparece em tudo:

- **dashboard**: seção "Composição do faturamento", ao lado da de despesas —
  mesmo componente, porque é a mesma figura (um total repartido em partes com
  nome);
- **bot**: `faturamento_por_categoria` no payload de `get_analysis`, no
  `get_resumo_mensal` e nas listagens com período (`search_entries`,
  `list_due_entries`);
- **resumo diário do WhatsApp**: sai de graça, porque `DigestPayload` deriva de
  `ToolPayload`.

**A origem continua decidindo sozinha o que é faturamento** (ADR-016). A
categoria só reparte o que já é venda: uma categoria nova de entrada não vira
faturamento por existir, e um empréstimo arquivado em "Venda Balcão" continua
não sendo venda. É a diferença entre "quanto vendi" (origem) e "que tipo de
venda foi" (categoria), e o prompt do agente diz isso com essas palavras.

A quebra decompõe exatamente `KPIs.Faturamento` — a mesma leitura na base de
transação, o mesmo mês —, então as partes somam o total impresso acima delas. E
ela é cortada em 5 categorias no payload do bot com aviso e seção
(`faturamento_completo`) para o resto, pela regra da ADR-015.

### 4. O nome da categoria vem do catálogo do usuário

Uma quebra nomeia categorias, e metade delas agora são palavras que o usuário
escolheu. `Assemble` passou a ler `ListCategories` (uma Query pequena) e a
análise nomeia cada fatia com o label do dono. A leitura é só para nomes: se
falhar, cai nos padrões e depois no slug em Title Case, e a análise sai igual —
perder um acento não vale perder o mês.

## Consequências

- **Uma Query a mais** por lançamento (validação), por listagem com período, por
  `get_resumo_mensal` e por análise. São Queries pequenas na partição do
  usuário; ao lado das ~14 que uma análise já faz, é ruído — e o teto de custo da
  ADR-008 não sente.
- **`SchemaVersion` foi para 10.** Um snapshot v9 lido nesta struct não tem
  `revenueComposition`, e composição vazia é como este pacote diz "não vendeu
  nada" — o dashboard serve o snapshot armazenado, então sem o bump um mês de
  atacado e balcão apareceria como um mês sem vendas.
- **`analytics.LedgerReader` e `notifier.LedgerReader` ganharam
  `ListCategories`.** Continuam sendo fatias declaradas (ADR-014), uma linha
  maior.
- **`ExpenseComposition` virou `CategoryComposition` em Go** — um tipo para as
  duas quebras, porque é uma forma só. O JSON não mudou.
- **O modelo pode criar categoria demais.** O prompt manda criar só quando o
  usuário pede a separação, `create_category` reporta duplicata em vez de
  regravar (`SaveCategory` é `PutItem`: recriar renomearia de volta) e há um teto
  de 80 categorias por usuário.
- **Não há como apagar nem renomear categoria pelo bot.** Renomear pelo
  dashboard já funciona; apagar continua não existindo em lugar nenhum, e uma
  categoria com lançamentos não pode simplesmente sumir. Fica para quando
  alguém precisar.
