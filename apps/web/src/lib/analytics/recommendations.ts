import {
  RecommendationSeverity,
  type Recommendation,
  type WeekComparison,
  type GoalProgress,
  type Trends,
  type CashPosition,
} from './types'

type RecommendationInput = {
  weekComparison: WeekComparison
  goals: GoalProgress
  trends: Trends
  cashPosition: CashPosition
}

export function getRecommendations(input: RecommendationInput): Recommendation[] {
  const { weekComparison, goals, trends, cashPosition } = input
  const recs: Recommendation[] = []

  if (goals.revenueTarget > 0 && goals.daysRemaining > 0 && goals.daysTotal > 0) {
    const elapsed = goals.daysTotal - goals.daysRemaining
    if (elapsed > 0) {
      const currentDailyRate = goals.revenueActual / elapsed
      const neededPerDay = (goals.revenueTarget - goals.revenueActual) / goals.daysRemaining
      const onTrack = currentDailyRate >= neededPerDay * 1.05

      const weekPct = weekComparison.previousUpToDay !== 0
        ? ((weekComparison.current - weekComparison.previousUpToDay) / weekComparison.previousUpToDay) * 100
        : 0

      const weekImproved = weekPct > 5
      const weekBehind = weekPct < -5

      if (weekImproved && onTrack) {
        recs.push({
          severity: RecommendationSeverity.Success,
          title: 'Ritmo subiu e fecha a meta',
          message: 'O faturamento desta semana está acima da anterior. Mantenha esse ritmo para sustentar a projeção do mês.',
        })
      } else if (weekImproved && !onTrack) {
        recs.push({
          severity: RecommendationSeverity.Warning,
          title: 'Ritmo subiu mas ainda falta',
          message: neededPerDay > 0
            ? `Melhorou vs semana passada, mas precisa de ${formatBRL(neededPerDay)}/dia nos próximos ${goals.daysRemaining} dias para bater a meta.`
            : 'Melhorou vs semana passada. Continue nesse ritmo.',
        })
      } else if (weekBehind && onTrack) {
        recs.push({
          severity: RecommendationSeverity.Warning,
          title: 'Caiu mas a projeção fecha',
          message: 'O faturamento desta semana está abaixo da anterior, mas a projeção do mês ainda está dentro da meta. Recupere o ritmo.',
        })
      } else if (weekBehind && !onTrack) {
        recs.push({
          severity: RecommendationSeverity.Danger,
          title: 'Faturamento caiu e não bate a meta',
          message: neededPerDay > 0
            ? `Precisa de ${formatBRL(neededPerDay)}/dia nos próximos ${goals.daysRemaining} dias para atingir a meta do mês.`
            : 'Faturamento caiu mas já superou a meta do mês.',
        })
      } else {
        // stable (±5%)
        if (onTrack) {
          recs.push({
            severity: RecommendationSeverity.Success,
            title: 'Ritmo estável e dentro da projeção',
            message: 'O desempenho está consistente com a semana passada. Mantenha para preservar a projeção do mês.',
          })
        } else {
          recs.push({
            severity: RecommendationSeverity.Warning,
            title: 'Ritmo estável mas não é suficiente',
            message: neededPerDay > 0
              ? `Precisa acelerar para ${formatBRL(neededPerDay)}/dia nos próximos ${goals.daysRemaining} dias para bater a meta.`
              : 'Ritmo estável. Continue para preservar a projeção.',
          })
        }
      }
    }
  }

  if (trends.despesa.direction === 'up' && trends.despesa.change > 15) {
    recs.push({
      severity: RecommendationSeverity.Warning,
      title: 'Despesas acima do normal',
      message: `Cresceram ${Math.round(trends.despesa.change)}% vs mês passado. Revise gastos para manter a margem.`,
    })
  }

  if (trends.receita.direction === 'down' && Math.abs(trends.receita.change) > 10) {
    recs.push({
      severity: RecommendationSeverity.Danger,
      title: 'Receita caiu',
      message: `${Math.round(Math.abs(trends.receita.change))}% abaixo do mês passado. Identifique causas e aja rapidamente.`,
    })
  }

  if (cashPosition.daysUntilNegative !== null && cashPosition.daysUntilNegative <= 7) {
    recs.push({
      severity: RecommendationSeverity.Danger,
      title: 'Saldo fica negativo em breve',
      message: `O saldo fica negativo em ${cashPosition.daysUntilNegative} dia${cashPosition.daysUntilNegative > 1 ? 's' : ''}. Reduza despesas ou antecipe recebimentos.`,
    })
  }

  return recs
}

function formatBRL(value: number): string {
  return new Intl.NumberFormat('pt-BR', {
    style: 'currency',
    currency: 'BRL',
  }).format(value / 100)
}
