const CURRENCY_SYMBOLS: Record<string, string> = {
  RUB: "₽",
};

// Formats a whole-currency-unit amount (e.g. 764.1 rubles, never minor units like kopecks) with a
// thousands separator and a fixed 2-decimal mantissa, e.g. formatMoney(1234.5, "RUB") -> "1 234.50 ₽".
export function formatMoney(amount: number, currency: string): string {
  const [intPart, decPart] = amount.toFixed(2).split(".");
  const negative = intPart.startsWith("-");
  const digits = negative ? intPart.slice(1) : intPart;
  const grouped = digits.replace(/\B(?=(\d{3})+(?!\d))/g, " ");
  const symbol = CURRENCY_SYMBOLS[currency] ?? ` ${currency}`;
  return `${negative ? "-" : ""}${grouped}.${decPart}${symbol}`;
}
