import type { ComponentChildren } from "preact";
import { UserPlus, CreditCard, Gift, Wrench, Bell, type LucideIcon } from "lucide-preact";
import type { ActivityEvent } from "../api/types";
import { TelegramUserLink } from "../components/TelegramUserLink";

// Shared icon + label renderer per activity event type — used by both the dashboard widget
// (ActivityFeed.tsx) and the full paginated activity page (routes/Activity.tsx) so the two never
// drift out of sync with each other.
export const TYPE_META: Record<ActivityEvent["type"], { icon: LucideIcon; label: (e: ActivityEvent) => ComponentChildren }> = {
  signup: { icon: UserPlus, label: (e) => <>Регистрация: <TelegramUserLink id={e.targetId} username={e.targetUsername} /></> },
  purchase: { icon: CreditCard, label: (e) => <>Оплата {e.detail} от <TelegramUserLink id={e.targetId} username={e.targetUsername} /></> },
  referral_bonus: {
    icon: Gift,
    label: (e) => (
      <>
        Реферальный бонус: <TelegramUserLink id={e.actorId} username={e.actorUsername} /> → <TelegramUserLink id={e.targetId} username={e.targetUsername} />
      </>
    ),
  },
  admin_action: { icon: Wrench, label: (e) => <>{e.detail} · пользователь <TelegramUserLink id={e.targetId} username={e.targetUsername} /></> },
  notification: {
    icon: Bell,
    label: (e) => (
      <>
        {e.detail} · <TelegramUserLink id={e.targetId} username={e.targetUsername} />
      </>
    ),
  },
};
