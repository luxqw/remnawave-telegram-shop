import { openTelegramProfile } from "../auth/telegram";

// Renders a Telegram ID, clickable through to the user's profile when a username is known.
// stopPropagation matters because most usages sit inside DataTable rows that already have an
// onRowClick handler. Without a username there's no reliable way to open a profile from inside a
// Telegram Mini App (see openTelegramProfile), so the ID renders as plain, non-interactive text.
export function TelegramUserLink(props: { id: number | null | undefined; username?: string | null }) {
  if (props.id === null || props.id === undefined) return <span class="mono">—</span>;

  if (!props.username) {
    return (
      <span class="mono tg-link-disabled" title="У пользователя нет username — открыть профиль нельзя">
        {props.id}
      </span>
    );
  }

  const username = props.username;
  return (
    <a
      class="mono tg-link"
      href={`https://t.me/${username}`}
      onClick={(e) => {
        e.preventDefault();
        e.stopPropagation();
        openTelegramProfile(username);
      }}
      title={`Открыть @${username} в Telegram`}
    >
      <span class="tg-link-icon">↗</span>
      {props.id}
    </a>
  );
}
