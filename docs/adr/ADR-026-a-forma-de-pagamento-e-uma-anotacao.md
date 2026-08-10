# ADR-026: A forma de pagamento é uma anotação, não uma taxonomia

## Status

Accepted

## Contexto

O razão sabia **quando** uma conta foi quitada (`PaymentDate`) e **se** foi
(`PaymentStatus`), mas não sabia **como**. Perguntado "esse boleto do fornecedor
saiu do caixa ou foi no pix?", o sistema não tinha o dado em lugar nenhum — nem
o dashboard, nem o bot, nem a conferência de fim de dia.

Já existe um `payments.PaymentMethod` no code base (`credito`, `debito`, `pix`,
`boleto`, `outros`), e ele é uma coisa diferente: é **a classificação que a
adquirente dá a uma venda** ao importar o arquivo do PagBank (ADR-013), mapeada
pelo parser a partir dos códigos do provedor. Ela descreve como o cliente pagou
a farmácia numa maquininha, não como a farmácia escolheu pagar o fornecedor. Os
dois conjuntos até se parecem, mas um é sobre dinheiro que chegou por um canal
que nós não controlamos e o outro é sobre uma decisão de quem paga. Também não
podia ser reaproveitado literalmente: `packages/payments` importa
`packages/domain`, então o razão não pode importar de volta.

A pergunta de desenho, então, era o quanto estruturar. Havia três caminhos:
enum fechado no domínio (como `IncomeOrigin`), catálogo por usuário (como as
categorias, ADR-024) ou contas nomeadas ("Nubank PJ", "Caixa da loja"), que é
o começo de conciliação bancária.

## Decisão

**Texto livre, opcional, gravado quando o usuário quiser — e mais nada.**

`domain.FinancialEntry` ganha `PaymentMethod string`. Sem enum, sem catálogo,
sem tabela, sem normalização de slug, sem migração. O campo existe para o dono
da farmácia se orientar ao olhar a própria lista, não para o sistema tirar
conclusão dele.

Isso é escolha, não preguiça — e é o ponto que esta ADR existe para registrar,
porque um leitor futuro vai olhar um campo de texto livre e querer "consertar"
para enum.

### 1. A forma de pagamento vive com a data de pagamento

Ela é um fato sobre a quitação, então nasce e morre com `PaymentDate`.
`Normalize()` limpa o campo no mesmo ramo que já zera a data quando o
lançamento volta a ser `pending`: desmarcar uma conta como paga não pode deixar
um "pix" pendurado numa conta que ninguém pagou.

A consequência aceita é que **uma conta pendente não guarda forma planejada**.
"Esse aluguel sai em débito automático dia 10" é previsão, e previsão não é o
que este campo responde.

### 2. Nada valida o conteúdo, e vazio é o estado mais comum

`Validate()` não rejeita forma de pagamento nenhuma. O motivo é o mesmo que já
está escrito ao lado de `Origin`: `itemToEntry` roda `Validate` em **toda
leitura**, então uma regra apertada aqui transforma uma linha esquisita numa
falha do `ListEntries` inteiro — o usuário perde o mês para não perder um
campo decorativo.

O tamanho é limitado nas **bordas** (handler HTTP e tools do agente), onde há
um humano para corrigir e um 400 para devolver: `maxPaymentMethodLen` = 60
runas. `Normalize()` só apara espaço em volta.

Vazio não é ausência de dado a ser preenchida depois: é a resposta normal.
Todo lançamento anterior a esta mudança está vazio e vai continuar vazio — não
há backfill, diferente do que `Origin` precisou.

### 3. Vale para os dois sentidos

O campo é o mesmo em despesa e em receita; só o rótulo muda na interface
("Forma de pagamento" / "Forma de recebimento"). Custou uma linha a mais e
responde "esse crediário o cliente quitou como?", que é a mesma pergunta virada
para o outro lado.

### 4. Sugerir sem restringir

Texto livre deriva: "pix", "PIX" e "Pix " viram três coisas. A defesa é um
`<datalist>` com as formas que **este navegador** já usou, seguidas das comuns
que ele ainda não usou, e a última usada já pré-preenchida no diálogo de quitar
(`apps/web/src/lib/payment-methods.ts`). Digitar continua livre; repetir fica
mais fácil que divergir.

A lista de recentes mora no `localStorage`, não no razão, porque ela é
conveniência de digitação e não dado: perdê-la custa algumas teclas, e guardá-la
no servidor significaria tabela, endpoint e Query para algo que o usuário
redigita em quatro letras. As grafias são dobradas sem acento de caixa ao
guardar, então "pix" digitado hoje não fica ao lado do "Pix" de ontem.

O diálogo de confirmação que já existia continua confirmando com um clique: o
campo vem preenchido com a última forma e "Confirmar" funciona vazio.

### 5. O bot registra o que ouviu e não interroga

`create_financial_entry` e `edit_financial_entry` ganham `forma_pagamento`,
string livre e opcional. "Paguei o fornecedor no pix" grava "pix"; "paguei o
fornecedor" grava vazio e a conversa segue. O prompt proíbe as duas tentações:
inventar a forma e perguntar por ela.

## Consequências

- **Não existe "quanto saiu no pix"**, e não vai existir enquanto o campo for
  livre. Nenhum consumidor — dashboard, análise, resumo diário — soma ou agrupa
  por forma de pagamento. Somar texto livre é publicar um número que não se
  sustenta, que é justamente o que as ADR-016 e ADR-019 vetam.
- **O caminho de volta está aberto e fica mais barato com o tempo.** Se um dia a
  pergunta "quanto saiu no pix" importar, os valores já digitados são a lista
  inicial do enum, e aí se paga uma normalização com dado real em mãos, em vez
  de adivinhar hoje as sete opções certas.
- **Nenhuma migração, nenhum índice, nenhuma leitura a mais.** É um atributo
  novo no item do DynamoDB; ausente lê como vazio. O teto de custo da ADR-008
  não sente nada.
- **Não há filtro por forma de pagamento** em `EntryFilter`. Um filtro obrigaria
  a estender o `dynamotest` (ADR-014) para servir uma consulta que ninguém faz
  sobre um campo que não agrega.
- **`payments.PaymentMethod` continua existindo e separado.** São dois campos
  com nome parecido e dono diferente: um é da adquirente, outro é do usuário.
  Uni-los exigiria que a farmácia aceitasse o vocabulário da maquininha para
  descrever o próprio caixa.
