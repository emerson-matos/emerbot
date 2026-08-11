import { format, isValid, parseISO } from "date-fns";
import { ptBR } from "date-fns/locale";
import { formatBRL } from "@/lib/format";
import type { Devedor } from "@/api/types";

/**
 * O vocabulário do caderninho, num lugar só (ADR-027).
 *
 * Fiado não vence, envelhece: nada foi combinado, então nada está atrasado.
 * Toda frase que a tela usa para falar de uma dívida sai daqui, para que
 * "vencido", "atrasado" e "inadimplente" não voltem por uma porta lateral.
 */

/**
 * Como se diz a idade de uma dívida.
 *
 * `dias` vem pronto do backend — não é contado aqui, porque o dia de hoje é o
 * da farmácia e não o do navegador. Zero não é ausência de dado: é uma dívida
 * que começou hoje, e pela ADR-017 um dia em curso ainda não é um dia medido,
 * então o "há N dias" só começa em 1. `null` é conta sem dívida aberta.
 */
export function agingLabel(dias: number | null | undefined): string | null {
  if (dias === null || dias === undefined) return null;
  if (dias <= 0) return "começou hoje";
  return `em aberto há ${dias} ${dias === 1 ? "dia" : "dias"}`;
}

/**
 * Os três estados de uma conta. Crédito não é erro nem dívida negativa: é o
 * cliente que pagou mais do que devia, e o caderninho registra porque
 * aconteceu de verdade.
 */
export type ContaEstado = "devendo" | "quite" | "credito";

export function contaEstado(saldo: number): ContaEstado {
  if (saldo > 0) return "devendo";
  if (saldo < 0) return "credito";
  return "quite";
}

/**
 * O saldo como se lê.
 *
 * Crédito não é dívida negativa. Um "−R$ 50,00" no meio de uma lista de
 * dívidas é lido como desconto do total, e o total não é esse — então o
 * crédito é dito por extenso, com o valor no positivo: "R$ 50,00 em crédito".
 */
export function saldoTexto(saldo: number): string {
  if (contaEstado(saldo) === "credito")
    return `${formatBRL(-saldo)} em crédito`;
  return formatBRL(saldo);
}

/**
 * A linha embaixo do nome, na lista e no topo do extrato.
 *
 * Só quem está devendo tem idade. Um cliente em crédito não está "devendo há N
 * dias" — ele não está devendo — e o backend manda `dias_em_aberto: null`
 * justamente nesse caso; a legenda diz o que houve em vez de deixar o buraco.
 */
export function contaLegenda(
  devedor: Pick<Devedor, "saldo" | "dias_em_aberto">,
): string {
  switch (contaEstado(devedor.saldo)) {
    case "credito":
      return "pagou mais do que devia";
    case "quite":
      return "sem nada em aberto";
    default:
      // Sem `dias_em_aberto` a conta ainda está aberta; o que falta é a idade,
      // e inventá-la aqui seria contar o tempo no relógio errado.
      return agingLabel(devedor.dias_em_aberto) ?? "em aberto";
  }
}

/**
 * O sinal é o tipo: positivo é o que a pessoa levou, negativo é o que ela
 * pagou. Não existe campo `tipo` no movimento, e não deve existir um aqui —
 * esta função é a leitura do sinal, não um segundo discriminador.
 */
export type MovimentoTipo = "divida" | "pagamento";

export function movimentoTipo(valor: number): MovimentoTipo {
  return valor < 0 ? "pagamento" : "divida";
}

export const movimentoLabels: Record<MovimentoTipo, string> = {
  divida: "levou fiado",
  pagamento: "pagou",
};

/** "12/06/2026". */
export function formatFiadoDate(iso: string): string {
  const parsed = parseISO(iso);
  return isValid(parsed) ? format(parsed, "dd/MM/yyyy") : iso;
}

/**
 * "quinta-feira, 12 de junho" — o cabeçalho do dia. Sem a primeira letra
 * maiúscula: quem capitaliza é o `first-letter:uppercase` no render, como o
 * resto do app faz com meses.
 */
export function formatDiaLabel(iso: string): string {
  const parsed = parseISO(iso);
  if (!isValid(parsed)) return iso;
  return format(parsed, "EEEE, d 'de' MMMM 'de' yyyy", { locale: ptBR });
}

/** O endereço de um devedor no app. */
export function devedorPath(cliente: string): string {
  return `/fiado/${encodeURIComponent(cliente)}`;
}
