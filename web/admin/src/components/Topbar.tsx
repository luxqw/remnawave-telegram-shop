import { useEffect, useState } from "preact/hooks";
import { api } from "../api/client";
import type { DashboardHealth } from "../api/types";

export function Topbar(props: { adminId: number | null; title: string; onMenuClick: () => void }) {
  const [health, setHealth] = useState<DashboardHealth | null>(null);

  useEffect(() => {
    let cancelled = false;
    const poll = async () => {
      try {
        const h = await api.get<DashboardHealth>("/admin/api/dashboard/health");
        if (!cancelled) setHealth(h);
      } catch {
        if (!cancelled) setHealth(null);
      }
    };
    poll();
    const interval = setInterval(poll, 30000);
    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, []);

  const initials = props.adminId ? String(props.adminId).slice(-2) : "--";
  const isHealthy = health?.status === "ok";

  return (
    <header class="topbar">
      <div class="row" style={{ gap: 12 }}>
        <button class="hamburger-btn" onClick={props.onMenuClick} aria-label="Меню">☰</button>
        <span class="page-title" style={{ fontSize: 15 }}>{props.title}</span>
      </div>
      <div class="row">
        <span class={`health-dot ${isHealthy ? "" : "down"}`} title={health ? `${health.status}` : "unknown"} />
        <div class="identity-chip">
          <div class="identity-avatar mono">{initials}</div>
        </div>
      </div>
    </header>
  );
}
