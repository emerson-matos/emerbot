# ADR-030: Uma previsão nova é experimental e ganha no backtest

## Status

Accepted

Refina o ADR-019. Não troca a projeção que opera o produto; estabelece como uma
hipótese de previsão entra, é observada e pode vir a substituí-la.

## Contexto

O faturamento de uma farmácia não é plano: segunda, sábado e domingo têm ritmos
distintos. O ADR-019 já resolveu o erro de achatar esses dias: a projeção usa a
média gaussiana de cada dia da semana nas oito semanas completas anteriores.
Oito semanas são aproximadamente sessenta dias, mas são escolhidas como semanas
inteiras para que cada dia tenha exatamente oito oportunidades de observação.

Ainda falta distinguir duas perguntas que a mesma média não responde:

1. quanto uma terça-feira normalmente vale; e
2. se as terças recentes estão rodando acima ou abaixo desse normal.

Uma alternativa candidata usa a primeira como baseline `X` e mede nos dias
fechados recentes o quociente `realizado / X` do próprio dia da semana. A mediana
desses quocientes é o nível recente `X'`; os dias restantes são precificados por
`X × X'`. A mediana, em vez da média, impede que uma venda excepcional defina o
fechamento do mês.

Isso é uma hipótese, não uma verdade de produto. Publicá-la diretamente como a
projeção mudaria alertas, metas, recomendações e mensagens antes de saber se ela
erra menos que a base gaussiana. Também seria perigoso encaixá-la em `Analysis`:
esse payload é o contrato do dashboard e do bot e é guardado em snapshots
diários; um campo de experimento não deve invalidar uma leitura operacional.

## Decisão

### 1. A projeção oficial continua única

`Projection` e o seu uso em saúde, recomendações, metas diárias, caixa e
WhatsApp não mudam. O frontend nunca calcula nenhuma das duas previsões.

O experimento é servido por uma leitura própria, `projection-experiment`, e
aparece na página Análise numa seção marcada **Em teste**. Ela mostra:

- a projeção oficial e a experimental lado a lado;
- o nível recente em percentual e o número de dias fechados que o sustentam;
- a validação histórica dos dois métodos.

A seção "A semana da farmácia" mostra também o erro médio histórico de cada dia
da semana. Cada leitura é estritamente fora da amostra: uma segunda fechada é
comparada com a previsão que as oito semanas terminadas no domingo anterior
teriam dado, nunca com uma média que já contém aquela própria segunda.

Não entra no dashboard resumido, no digest, em ferramentas do agente ou em
snapshots. Assim uma falha, uma janela insuficiente ou um método perdedor não
altera uma decisão de produção.

### 2. Um baseline, um estimador de nível

O baseline `X` é o `dailyRates` já calculado para a projeção oficial: média
gaussiana por dia da semana nas oito semanas completas. Não há uma segunda média,
uma constante de sessenta dias concorrente, nem cálculo no browser.

O nível `X'` olha os últimos 21 dias **fechados** disponíveis. Para cada dia cujo
baseline é positivo, soma o faturamento do dia e calcula `realizado / X`. A
mediana desses valores é o fator aplicado aos dias restantes. Dias cujo baseline
é zero são dias sem operação conhecida: não dividem por zero e não contam nem
como sucesso nem como queda de regime.

O dia corrente continua sendo parcial: a previsão credita somente o que ainda
falta depois do que ele já realizou, tal como a projeção oficial. Dia futuro é
sempre precificado pelo seu próprio dia da semana. Este estimador não cria nem
altera `Plan` ou `DayTarget`; a regra de piso do ADR-025 permanece exclusiva da
meta, não da previsão.

### 3. A promoção depende de backtest reproduzível

Para cada mês fechado e para os cortes dos dias 5, 10, 15 e 20, o backtest se
coloca naquele corte: usa somente lançamentos que já estavam fechados na data,
calcula os dois fechamentos e os compara ao faturamento efetivamente fechado do
mês. Ele publica o erro absoluto de cada método, o erro médio e as vitórias por
corte.

O cálculo é uma função pura de `analytics`. O serviço consulta um intervalo de
lançamentos uma vez e a reutiliza em todos os cortes; cada corte não pode fazer
sua própria leitura nem reimplementar a projeção. Meses/cortes sem histórico
suficiente são explicitamente excluídos, nunca preenchidos com zero.

Trocar a projeção oficial exigirá uma ADR posterior com evidência do backtest.
Não há limiar escondido no código: a evidência é mostrada, e a decisão é de
produto.

## Consequências

- A API operacional `GET /analysis/monthly`, seu `SchemaVersion` e os snapshots
  permanecem compatíveis.
- A nova API é deliberadamente de observação, com seu tipo de resposta próprio;
  ela não é um campo opcional de `Analysis`.
- `dailyRates` continua dono de ritmo e evidência por dia da semana. O
  estimador experimental recebe esse baseline, evitando divergência matemática.
- O backtest adiciona leitura e CPU apenas quando a seção experimental é aberta;
  não pesa notificações nem a montagem normal da análise.
- Uma previsão experimental pode ficar indisponível por histórico insuficiente.
  Essa ausência é uma resposta honesta, não uma previsão de R$ 0,00.
