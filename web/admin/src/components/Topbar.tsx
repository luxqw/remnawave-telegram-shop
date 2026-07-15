import { useEffect, useState } from "preact/hooks";
import { api } from "../api/client";
import type { DashboardHealth } from "../api/types";
import { getCurrentTheme, toggleTheme, type Theme } from "../lib/theme";
import { openCommandPalette } from "./CommandPalette";
import { HeaderStats } from "./HeaderStats";

function SunIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M4.93 19.07l1.41-1.41M17.66 6.34l1.41-1.41" />
    </svg>
  );
}

function MoonIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z" />
    </svg>
  );
}

export function Topbar(props: { adminId: number | null; title: string; onMenuClick: () => void }) {
  const [health, setHealth] = useState<DashboardHealth | null>(null);
  const [theme, setTheme] = useState<Theme>(() => getCurrentTheme());

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
        <HeaderStats />
        <button
          class="btn btn-ghost btn-sm mono"
          onClick={openCommandPalette}
          aria-label="Быстрый поиск"
          title="Быстрый поиск (⌘K)"
        >
          ⌘K
        </button>
        <button
          class="btn btn-ghost theme-toggle-btn"
          onClick={() => setTheme(toggleTheme())}
          aria-label={theme === "dark" ? "Включить светлую тему" : "Включить тёмную тему"}
          title={theme === "dark" ? "Включить светлую тему" : "Включить тёмную тему"}
        >
          {theme === "dark" ? <SunIcon /> : <MoonIcon />}
        </button>
        <span class={`health-dot ${isHealthy ? "" : "down"}`} title={health ? `${health.status}` : "unknown"} />
        <div class="identity-chip">
          <div class="identity-avatar mono">{initials}</div>
        </div>
      </div>
    </header>
  );
}
