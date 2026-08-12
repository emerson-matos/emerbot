# ---------------------------------------------------------------------------
# A guarda de custo do ADR-028.
#
# A arquitetura assíncrona é adotada *enquanto* o custo mensal total ficar
# abaixo do teto do ADR-008 (R$20/mês). Uma regra assim depende de alguém olhar
# o Cost Explorer, e uma regra que depende de alguém lembrar não é guarda, é
# intenção — por isso ela vem com gatilho: um orçamento com alerta.
#
# Dois orçamentos são gratuitos por conta; este é o primeiro. Ele é da conta
# inteira, não da fila: o que o ADR-028 promete não é que o SQS seja barato, é
# que o total continue cabendo.
#
# O alerta dispara em dois pontos: 80% do previsto (ainda dá para agir dentro do
# mês) e 100% do real (aconteceu). Quando o alerta chegar, o ADR manda revisar
# antes de trocar o mecanismo: concorrência máxima e pollers do event source
# mapping (scaling_config em modules/api_gateway_lambda).
#
# Sem budget_alert_email não há para quem avisar, e um orçamento que não avisa
# ninguém é um número no console — então ele nem é criado. `tofu plan` mostra a
# ausência; ver docs/deploy.md.
# ---------------------------------------------------------------------------
resource "aws_budgets_budget" "monthly_cap" {
  count = var.budget_alert_email != "" ? 1 : 0

  name         = "${var.project_name}-${var.environment}-monthly"
  budget_type  = "COST"
  limit_amount = var.budget_monthly_limit_usd
  # A AWS orça em dólar; o teto do ADR-008 é em real, então este valor é a
  # conversão folgada dele — a intenção é disparar antes de R$20, não acertar a
  # cotação do dia.
  limit_unit        = "USD"
  time_unit         = "MONTHLY"
  time_period_start = "2026-01-01_00:00"

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 80
    threshold_type             = "PERCENTAGE"
    notification_type          = "FORECASTED"
    subscriber_email_addresses = [var.budget_alert_email]
  }

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    notification_type          = "ACTUAL"
    subscriber_email_addresses = [var.budget_alert_email]
  }
}
