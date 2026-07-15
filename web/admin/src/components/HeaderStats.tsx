import { useEffect, useState } from "preact/hooks";
import { api } from "../api/client";
import type { HeaderStats as HeaderStatsData } from "../api/types";
import { formatMoney } from "../lib/format";

// Compact metrics strip in the Topbar: active subscriptions, a trailing-30-day revenue
// approximation (explicitly not "MRR" in the strict recurring-revenue sense — there's no
// per-customer recurring-amount tracking to compute that from), and today's expiration count
// (deliberately not "churn" — expiring today isn't the same as actually lapsing). Plain numbers,
// no sparkline — a historical trend line would need new time-series queries beyond this pass's
// scope.
export function HeaderStats() {
  const [stats, setStats] = useState<HeaderStatsData | null>(null);

  useEffect(() => {
    let cancelled = false;
    api
      .get<HeaderStatsData>("/admin/api/dashboard/header-stats")
      .then((s) => { if (!cancelled) setStats(s); })
      .catch(() => { if (!cancelled) setStats(null); });
    return () => { cancelled = true; };
  }, []);

  if (!stats) return null;

  return (
    <div class="header-stats" title="MRR (30 дней) — приблизительная выручка за последние 30 дней, не строгий recurring MRR">
      <div class="header-stats-item">
        <span class="header-stats-value mono">{stats.activeSubscriptions}</span>
        <span class="header-stats-label">Активные подписки</span>
      </div>
      <div class="header-stats-item">
        <span class="header-stats-value mono">{formatMoney(stats.mrr30d, stats.mrrCurrency)}</span>
        <span class="header-stats-label">MRR (30 дней)</span>
      </div>
      <div class="header-stats-item">
        <span class="header-stats-value mono">{stats.expiringToday}</span>
        <span class="header-stats-label">Истекает сегодня</span>
      </div>
    </div>
  );
}
