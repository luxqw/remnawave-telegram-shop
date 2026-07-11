import { useEffect, useState } from "preact/hooks";
import type { ComponentChildren } from "preact";
import { api } from "../api/client";
import type { ActivityEvent } from "../api/types";
import { GlassCard } from "./GlassCard";
import { TelegramUserLink } from "./TelegramUserLink";

const TYPE_META: Record<ActivityEvent["type"], { icon: string; label: (e: ActivityEvent) => ComponentChildren }> = {
  signup: { icon: "◍", label: (e) => <>Регистрация: <TelegramUserLink id={e.targetId} username={e.targetUsername} /></> },
  purchase: { icon: "💰", label: (e) => <>Оплата {e.detail} от <TelegramUserLink id={e.targetId} username={e.targetUsername} /></> },
  referral_bonus: {
    icon: "🎁",
    label: (e) => (
      <>
        Реферальный бонус: <TelegramUserLink id={e.actorId} username={e.actorUsername} /> → <TelegramUserLink id={e.targetId} username={e.targetUsername} />
      </>
    ),
  },
  admin_action: { icon: "🛠", label: (e) => <>{e.detail} · пользователь <TelegramUserLink id={e.targetId} username={e.targetUsername} /></> },
};

export function ActivityFeed() {
  const [events, setEvents] = useState<ActivityEvent[] | null>(null);

  useEffect(() => {
    api.get<ActivityEvent[]>("/admin/api/dashboard/activity?limit=50").then(setEvents).catch(() => setEvents([]));
  }, []);

  return (
    <GlassCard>
      <div class="stat-tile-label" style={{ marginBottom: 12 }}>Лента активности</div>
      {events === null && <div class="shimmer" style={{ height: 120 }} />}
      {events?.length === 0 && <div class="page-subtitle">Пока нет событий</div>}
      {events && events.length > 0 && (
        <div class="stack" style={{ gap: 10 }}>
          {events.map((e, i) => {
            const meta = TYPE_META[e.type];
            return (
              <div class="data-card-row" key={i}>
                <span class="data-card-label">{meta.icon} {new Date(e.timestamp).toLocaleString("ru-RU")}</span>
                <span class="data-card-value">{meta.label(e)}</span>
              </div>
            );
          })}
        </div>
      )}
    </GlassCard>
  );
}
