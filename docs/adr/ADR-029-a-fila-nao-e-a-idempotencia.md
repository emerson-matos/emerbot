# ADR-029: A fila não é a idempotência

## Status

Accepted

## Contexto

O ADR-028 põe uma fila SQS FIFO entre o webhook e o worker, e o FIFO tem
deduplicação nativa: mensagens com o mesmo `MessageDeduplicationId` dentro de
uma janela são descartadas. É tentador concluir que o marcador de dedup no
DynamoDB (`packages/wasession`: `MarkProcessed` / `Unmark`) deixou de ser
necessário.

Não deixou, e essa conclusão foi feita e desfeita durante o refinamento — vale
registrar por quê.

São dois problemas com o mesmo nome:

- **"esta mensagem chegou duas vezes agora"** — retentativa de transporte,
  minutos, resolvida perto do transporte;
- **"nós já processamos esta mensagem"** — fato do domínio, sem prazo,
  verdadeiro para sempre depois que a resposta foi enviada.

A janela de deduplicação do FIFO é curta e é uma propriedade do serviço. Fazer a
consistência do domínio depender dela significa que a resposta para "já
respondemos ao João?" expira sozinha, e que trocar o transporte um dia leva a
garantia junto.

Some-se que a entrega do SQS para Lambda é **at-least-once**: a própria AWS
recomenda que o processamento seja idempotente. A fila reduz duplicatas; não as
elimina.

## Decisão

As duas camadas coexistem, com papéis nomeados.

```
webhook
   │  MessageDeduplicationId = message ID da Meta
   ▼
SQS FIFO  ── dedup de transporte (janela curta)
   │
   ▼
worker
   │  escrita condicional no DynamoDB
   ▼
processa uma vez
```

**O FIFO deduplica o transporte.** `MessageDeduplicationId` = o `message ID` da
Meta, que é o identificador que a Meta repete quando retenta. Isso descarta a
retentativa antes de ela custar uma invocação.

**O DynamoDB responde à pergunta do domínio.** Uma escrita condicional
(`attribute_not_exists`) no ID da mensagem: quem escreve, processa; quem falha a
condição, já foi processada por alguém. É a mesma tabela e o mesmo item de hoje,
com o TTL que ela já tem.

**O `Unmark` deixa de existir.** Ele era compensação: o webhook reivindicava o
marcador antes de processar e o devolvia se o turno falhasse, para a retentativa
da Meta não ser engolida. Isso existia porque o webhook era o único lugar onde a
entrega podia ser garantida — e em produção nunca funcionou, porque a policy não
tinha `dynamodb:DeleteItem` e todo retry era respondido "ignoring duplicate".

Com a fila, a entrega é responsabilidade dela: uma tentativa que falha volta
para a fila e é reentregue, sem ninguém precisar desfazer nada. **A marca passa
a ser escrita quando o processamento termina, não quando começa** — o que a
torna o fato que ela sempre deveria ter sido, e não uma reserva que precisa de
desfazimento.

O preço disso: duas tentativas concorrentes da *mesma* mensagem poderiam ambas
processar antes de qualquer uma marcar. O `MessageGroupId` por telefone
(ADR-028) serializa o grupo, então isso exigiria a mesma mensagem em voo duas
vezes no mesmo grupo — que é o que a serialização impede.

## Consequências

- a garantia de "processar uma vez" sobrevive a uma troca de transporte, porque
  não mora no transporte
- a resposta para "já respondemos isso?" não expira junto com uma janela de
  serviço
- some o `Unmark`, some o `defer` compensatório, e some a permissão de
  `dynamodb:DeleteItem` que o PR #98 precisou conceder — o mecanismo não é
  remendado, deixa de ser necessário
- a marca passa a ser escrita no fim, o que muda o significado do item: ele
  deixa de ser reserva e vira registro
- `RecordInbound` (a janela de 24h do WhatsApp) não muda; é outro assunto na
  mesma tabela
- duas camadas de dedup é mais peça, e a justificativa tem de estar escrita —
  está aqui, porque a leitura natural é achar que uma delas sobra
