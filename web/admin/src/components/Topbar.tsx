import { useEffect, useState } from "preact/hooks";
import { Sun, Moon, Menu } from "lucide-preact";
import { api } from "../api/client";
import type { DashboardHealth } from "../api/types";
import { getCurrentTheme, toggleTheme, type Theme } from "../lib/theme";
import { openCommandPalette } from "./CommandPalette";
import { HeaderStats } from "./HeaderStats";

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
        <button class="hamburger-btn" onClick={props.onMenuClick} aria-label="Меню"><Menu size={20} /></button>
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
          {theme === "dark" ? <Sun size={18} /> : <Moon size={18} />}
        </button>
        <span class={`health-dot ${isHealthy ? "" : "down"}`} title={health ? `${health.status}` : "unknown"} />
        <div class="identity-chip">
          <div class="identity-avatar mono">{initials}</div>
        </div>
      </div>
    </header>
  );
}
