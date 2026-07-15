import { useEffect, useState } from "preact/hooks";
import { api } from "../api/client";
import type { DashboardStats, DashboardReferrals, DayPoint } from "../api/types";
import { GlassCard } from "../components/GlassCard";
import { StatTile } from "../components/StatTile";
import { ChartLine } from "../components/ChartLine";
import { ChartBar } from "../components/ChartBar";
import { ActivityFeed } from "../components/ActivityFeed";
import { formatMoney, pluralDays } from "../lib/format";

const DAY_RANGES = [7, 30, 90] as const;

export function Dashboard() {
  const [stats, setStats] = useState<DashboardStats | null>(null);
  const [revenue, setRevenue] = useState<DayPoint[] | null>(null);
  const [growth, setGrowth] = useState<DayPoint[] | null>(null);
  const [referrals, setReferrals] = useState<DashboardReferrals | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [days, setDays] = useState(30);

  useEffect(() => {
    Promise.all([
      api.get<DashboardStats>("/admin/api/dashboard/stats"),
      api.get<DayPoint[]>(`/admin/api/dashboard/revenue?days=${days}`),
      api.get<DayPoint[]>(`/admin/api/dashboard/growth?days=${days}`),
      api.get<DashboardReferrals>("/admin/api/dashboard/referrals"),
    ])
      .then(([s, r, g, ref]) => {
        setStats(s);
        setRevenue(r);
        setGrowth(g);
        setReferrals(ref);
      })
      .catch((err) => setError(err instanceof Error ? err.message : "Ошибка загрузки"));
  }, [days]);

  if (error) {
    return (
      <GlassCard>
        <div class="stat-tile-label" style={{ marginBottom: 6 }}>Не удалось загрузить дашборд</div>
        <div class="page-subtitle">{error}</div>
      </GlassCard>
    );
  }
  if (!stats || !revenue || !growth || !referrals) {
    return <div class="shimmer" style={{ height: 300 }} />;
  }

  const totalRevenue = revenue.reduce((acc, p) => acc + p.value, 0);

  return (
    <div class="stack">
      <div class="bento-grid">
        <div class="bento-main">
          <GlassCard>
            <div class="row" style={{ justifyContent: "space-between", marginBottom: 14 }}>
              <div>
                <div class="stat-tile-label">Выручка за {days} {pluralDays(days)}</div>
                <div class="stat-tile-value mono">{formatMoney(totalRevenue, "RUB")}</div>
              </div>
              <div class="days-toggle">
                {DAY_RANGES.map((d) => (
                  <button
                    key={d}
                    class={`days-toggle-btn ${days === d ? "active" : ""}`}
                    onClick={() => setDays(d)}
                  >
                    {d}д
                  </button>
                ))}
              </div>
            </div>
            <ChartLine points={revenue} height={220} days={days} />
          </GlassCard>
          <GlassCard style={{ marginTop: 16 }}>
            <div class="stat-tile-label" style={{ marginBottom: 14 }}>Новые пользователи / день</div>
            <ChartBar points={growth} height={120} days={days} />
          </GlassCard>
        </div>
        <div class="bento-side">
          <StatTile label="Всего пользователей" value={stats.total} />
          <StatTile label="Активные (оплаченные)" value={stats.activePaid} />
          <StatTile label="Активные (триал)" value={stats.activeTrial} />
          <StatTile label="Истёкшие" value={stats.expired} />
          <StatTile label="Без подписки" value={stats.noSub} />
        </div>
      </div>
      <GlassCard>
        <div class="stat-tile-label" style={{ marginBottom: 10 }}>Реферальная программа</div>
        <div class="row" style={{ gap: 32 }}>
          <StatTile label="Приглашений" value={referrals.total} />
          <StatTile label="Бонус начислен" value={referrals.granted} />
          <StatTile label="Конверсия" value={`${referrals.conversionPercent.toFixed(0)}%`} />
        </div>
      </GlassCard>
      <ActivityFeed />
    </div>
  );
}
