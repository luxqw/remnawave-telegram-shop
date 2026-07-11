// Renders a Telegram ID as a clickable deep link to that user's profile (tg://user?id=...). Used
// wherever a customer/admin telegram_id is displayed so admins can jump straight into a chat.
// stopPropagation matters because most usages sit inside DataTable rows that already have an
// onRowClick handler.
export function TelegramUserLink(props: { id: number | null | undefined }) {
  if (props.id === null || props.id === undefined) return <span class="mono">—</span>;
  return (
    <a
      class="mono tg-link"
      href={`tg://user?id=${props.id}`}
      target="_blank"
      rel="noreferrer"
      onClick={(e) => e.stopPropagation()}
      title="Открыть профиль в Telegram"
    >
      <span class="tg-link-icon">↗</span>
      {props.id}
    </a>
  );
}
