# ADR-009: State remoto e deploy via CI (GitHub OIDC)

## Status

Accepted

## Contexto

O deploy era manual e local: `make tofu-apply` na máquina do dev, com o
`terraform.tfstate` num único laptop. Isso concentra o risco (fator de ônibus 1),
deixa o state frágil (perda = recursos órfãos na AWS) e não mostra o `plan` nas
PRs de infra. O teto de custo (ADR-008) exige que a solução não adicione serviço
com custo fixo relevante.

## Decisão

- **State remoto no S3** (versionado, criptografado, acesso público bloqueado),
  com trava nativa via `use_lockfile` (OpenTofu >= 1.10) — sem tabela DynamoDB
  de lock rodando 24/7.
- **Deploy pelo GitHub Actions** autenticado por **OIDC** (papel IAM assumido em
  runtime), sem chaves AWS de longa duração armazenadas.
- **`apply` é botão manual** (`workflow_dispatch`); PRs só rodam `plan` e
  publicam o resultado como comentário.
- Bootstrap (bucket + OIDC provider + papel) fica em `infra/opentofu/bootstrap`,
  rodado uma vez por conta com credenciais de admin.

## Consequências

- fim do fator de ônibus 1 e do state em laptop
- revisão de mudanças de infra com o `plan` visível na PR
- sem segredos AWS estáticos (OIDC)
- custo adicional desprezível (state em S3 ~centavos; OIDC e Actions grátis nessa
  escala), coerente com ADR-008
- `apply` continua sendo uma ação deliberada, não automática no merge
- deploy local segue disponível como break-glass (mesmo state remoto)
