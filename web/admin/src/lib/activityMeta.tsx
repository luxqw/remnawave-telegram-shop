import type { ComponentChildren } from "preact";
import type { ActivityEvent } from "../api/types";
import { TelegramUserLink } from "../components/TelegramUserLink";

// Shared icon + label renderer per activity event type — used by both the dashboard widget
// (ActivityFeed.tsx) and the full paginated activity page (routes/Activity.tsx) so the two never
// drift out of sync with each other.
export const TYPE_META: Record<ActivityEvent["type"], { icon: string; label: (e: ActivityEvent) => ComponentChildren }> = {
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
  notification: {
    icon: "🔔",
    label: (e) => (
      <>
        {e.detail} · <TelegramUserLink id={e.targetId} username={e.targetUsername} />
      </>
    ),
  },
};
