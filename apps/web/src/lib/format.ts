export function formatBRL(centavos: number): string {
  return (centavos / 100).toLocaleString("pt-BR", {
    style: "currency",
    currency: "BRL",
  });
}

export function formatSignedBRL(centavos: number): string {
  const sign = centavos >= 0 ? "+ " : "− ";
  return sign + formatBRL(Math.abs(centavos));
}
