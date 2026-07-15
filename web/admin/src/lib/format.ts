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

// Russian noun pluralization for "day" (день/дня/дней), used by the Dashboard's 7/30/90-day
// range label. Standard mod-10/mod-100 Slavic pluralization rules.
export function pluralDays(n: number): string {
  const mod10 = n % 10;
  const mod100 = n % 100;
  if (mod10 === 1 && mod100 !== 11) return "день";
  if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) return "дня";
  return "дней";
}
