import { useEffect, useState } from "preact/hooks";
import { api, ApiError } from "../api/client";
import type { ActivityEvent, Page } from "../api/types";
import { GlassCard } from "../components/GlassCard";
import { DataTable, type Column } from "../components/DataTable";
import { Pagination } from "../components/Pagination";
import { useToast } from "../components/Toast";
import { TYPE_META } from "../lib/activityMeta";
import { navigate } from "../router";

const TYPES: { value: ActivityEvent["type"] | ""; label: string }[] = [
  { value: "", label: "Все типы" },
  { value: "signup", label: "Регистрация" },
  { value: "purchase", label: "Оплата" },
  { value: "referral_bonus", label: "Реферальный бонус" },
  { value: "admin_action", label: "Действие админа" },
  { value: "notification", label: "Уведомление" },
];

export function Activity() {
  const toast = useToast();
  const [page, setPage] = useState<Page<ActivityEvent> | null>(null);
  const [pageNum, setPageNum] = useState(1);
  const [type, setType] = useState("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");

  useEffect(() => {
    const params = new URLSearchParams({ page: String(pageNum), limit: "30" });
    if (type) params.set("type", type);
    if (from) params.set("from", from);
    if (to) params.set("to", to);
    api
      .get<Page<ActivityEvent>>(`/admin/api/activity?${params.toString()}`)
      .then(setPage)
      .catch((err) => {
        toast.push(err instanceof ApiError ? err.message : "Не удалось загрузить активность", "error");
        setPage({ items: [], total: 0, page: pageNum, limit: 30 });
      });
  }, [pageNum, type, from, to]);

  const exportUrl = () => {
    const params = new URLSearchParams();
    if (type) params.set("type", type);
    if (from) params.set("from", from);
    if (to) params.set("to", to);
    return `/admin/api/activity/export.csv?${params.toString()}`;
  };

  const columns: Column<ActivityEvent>[] = [
    {
      header: "Дата",
      render: (e) => new Date(e.timestamp).toLocaleString("ru-RU"),
      sortKey: "timestamp",
      sortValue: (e) => new Date(e.timestamp).getTime(),
    },
    {
      header: "Событие",
      render: (e) => {
        const Icon = TYPE_META[e.type].icon;
        return (
          <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
            <Icon size={16} /> {TYPE_META[e.type].label(e)}
          </span>
        );
      },
      sortKey: "type",
      sortValue: (e) => e.type,
    },
  ];

  return (
    <div class="stack">
      <div class="row">
        <select class="select" value={type} onChange={(e) => { setType((e.target as HTMLSelectElement).value); setPageNum(1); }}>
          {TYPES.map((t) => (
            <option key={t.value} value={t.value}>
              {t.label}
            </option>
          ))}
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
        <div class="spacer" />
        <a class="btn btn-sm" href={exportUrl()} target="_blank" rel="noreferrer">Экспорт CSV</a>
      </div>
      <GlassCard>
        {!page ? (
          <div class="shimmer" style={{ height: 200 }} />
        ) : (
          <>
            <DataTable
              columns={columns}
              rows={page.items}
              keyFn={(e) => `${e.type}-${e.timestamp}-${e.targetId}`}
              // admin_action rows don't reliably resolve to a real customer via targetId, so
              // clicking them is a no-op rather than navigating to a bogus user page.
              onRowClick={(e) => {
                if (e.type === "admin_action") return;
                navigate(`users/${e.targetId}`);
              }}
              emptyMessage="Событий не найдено"
            />
            <Pagination page={pageNum} limit={page.limit} total={page.total} onChange={setPageNum} />
          </>
        )}
      </GlassCard>
    </div>
  );
}
