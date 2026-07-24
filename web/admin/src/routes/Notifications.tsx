import { useEffect, useState } from "preact/hooks";
import { api, ApiError } from "../api/client";
import type { NotificationLogEntry, NotificationStats, Page } from "../api/types";
import { GlassCard } from "../components/GlassCard";
import { DataTable, type Column } from "../components/DataTable";
import { Pagination } from "../components/Pagination";
import { Badge } from "../components/Badge";
import { StatTile } from "../components/StatTile";
import { TelegramUserLink } from "../components/TelegramUserLink";
import { useToast } from "../components/Toast";

const TYPES = ["", "trial_expiring", "subscription_expiring", "traffic_warning", "broadcast", "admin_message"];
const STATUSES = ["", "sent", "failed", "skipped"];

// RESENDABLE_TYPES mirrors resendableNotificationTypes in internal/webapp/handlers_notifications.go:
// broadcast/admin_message only ever had a status/detail string logged, not the actual message
// text, so there's nothing meaningful to resend for those.
const RESENDABLE_TYPES = new Set(["trial_expiring", "subscription_expiring", "traffic_warning"]);

export function Notifications() {
  const toast = useToast();
  const [page, setPage] = useState<Page<NotificationLogEntry> | null>(null);
  const [pageNum, setPageNum] = useState(1);
  const [type, setType] = useState("");
  const [status, setStatus] = useState("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [stats, setStats] = useState<NotificationStats | null>(null);
  const [reloadTick, setReloadTick] = useState(0);
  const [resendingId, setResendingId] = useState<number | null>(null);

  useEffect(() => {
    const params = new URLSearchParams({ page: String(pageNum), limit: "30" });
    if (type) params.set("type", type);
    if (status) params.set("status", status);
    if (from) params.set("from", from);
    if (to) params.set("to", to);
    api
      .get<Page<NotificationLogEntry>>(`/admin/api/notifications?${params.toString()}`)
      .then(setPage)
      .catch((err) => {
        toast.push(err instanceof ApiError ? err.message : "Не удалось загрузить уведомления", "error");
        setPage({ items: [], total: 0, page: pageNum, limit: 30 });
      });
  }, [pageNum, type, status, from, to, reloadTick]);

  useEffect(() => {
    api
      .get<NotificationStats>("/admin/api/notifications/stats?days=7")
      .then(setStats)
      .catch((err) => toast.push(err instanceof ApiError ? err.message : "Не удалось загрузить статистику", "error"));
  }, [reloadTick]);

  const resend = async (n: NotificationLogEntry) => {
    setResendingId(n.id);
    try {
      await api.post(`/admin/api/notifications/${n.id}/resend`);
      toast.push(`Уведомление #${n.id} отправлено повторно`);
      setReloadTick((t) => t + 1);
    } catch (err) {
      toast.push(err instanceof ApiError ? err.message : "Ошибка повторной отправки", "error");
    } finally {
      setResendingId(null);
    }
  };

  const successPercent = stats && stats.total > 0 ? ((stats.sent / stats.total) * 100).toFixed(1) : "—";

  const columns: Column<NotificationLogEntry>[] = [
    {
      header: "Дата",
      render: (n) => new Date(n.createdAt).toLocaleString("ru-RU"),
      sortKey: "createdAt",
      sortValue: (n) => new Date(n.createdAt).getTime(),
    },
    {
      header: "Пользователь",
      render: (n) => <TelegramUserLink id={n.customerTelegramId} username={n.customerUsername} />,
      sortKey: "customerTelegramId",
      sortValue: (n) => n.customerTelegramId,
    },
    { header: "Тип", render: (n) => n.notificationType, sortKey: "notificationType", sortValue: (n) => n.notificationType },
    {
      header: "Статус",
      render: (n) => (
        <Badge variant={n.status === "sent" ? "success" : n.status === "failed" ? "danger" : "neutral"}>{n.status}</Badge>
      ),
      sortKey: "status",
      sortValue: (n) => n.status,
    },
    { header: "Детали", render: (n) => n.detail ?? "—" },
    { header: "Ошибка", render: (n) => n.errorMessage ?? "—" },
    {
      header: "",
      render: (n) =>
        n.status === "failed" && RESENDABLE_TYPES.has(n.notificationType) ? (
          <button
            class="btn btn-sm"
            disabled={resendingId === n.id}
            onClick={(e) => {
              e.stopPropagation();
              resend(n);
            }}
          >
            Повторить
          </button>
        ) : null,
    },
  ];

  return (
    <div class="stack">
      <div class="row" style={{ flexWrap: "wrap" }}>
        <StatTile label="Отправлено (7 дн.)" value={stats ? stats.sent : "…"} />
        <StatTile label="Ошибки (7 дн.)" value={stats ? stats.failed : "…"} />
        <StatTile label="Пропущено (7 дн.)" value={stats ? stats.skipped : "…"} />
        <StatTile label="Успешность" value={stats ? `${successPercent}%` : "…"} />
      </div>
      <div class="row">
        <select class="select" value={type} onChange={(e) => { setType((e.target as HTMLSelectElement).value); setPageNum(1); }}>
          {TYPES.map((t) => <option key={t} value={t}>{t || "Все типы"}</option>)}
        </select>
        <select class="select" value={status} onChange={(e) => { setStatus((e.target as HTMLSelectElement).value); setPageNum(1); }}>
          {STATUSES.map((s) => <option key={s} value={s}>{s || "Все статусы"}</option>)}
        </select>
        <input
          class="input"
          type="date"
          value={from}
          onInput={(e) => { setFrom((e.target as HTMLInputElement).value); setPageNum(1); }}
          style={{ maxWidth: 160 }}
        />
        <input
          class="input"
          type="date"
          value={to}
          onInput={(e) => { setTo((e.target as HTMLInputElement).value); setPageNum(1); }}
          style={{ maxWidth: 160 }}
        />
      </div>
      <GlassCard>
        {!page ? (
          <div class="shimmer" style={{ height: 200 }} />
        ) : (
          <>
            <DataTable columns={columns} rows={page.items} keyFn={(n) => n.id} emptyMessage="Уведомлений не найдено" />
            <Pagination page={pageNum} limit={page.limit} total={page.total} onChange={setPageNum} />
          </>
        )}
      </GlassCard>
    </div>
  );
}
