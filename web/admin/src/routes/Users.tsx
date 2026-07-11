import { useEffect, useState } from "preact/hooks";
import { api } from "../api/client";
import type { Customer, Page } from "../api/types";
import { GlassCard } from "../components/GlassCard";
import { DataTable, type Column } from "../components/DataTable";
import { Pagination } from "../components/Pagination";
import { Badge } from "../components/Badge";
import { TelegramUserLink } from "../components/TelegramUserLink";
import { navigate } from "../router";

const FILTERS = [
  { value: "", label: "Все" },
  { value: "active", label: "Активные" },
  { value: "trial", label: "Триал" },
  { value: "expired", label: "Истёкшие" },
  { value: "no_sub", label: "Без подписки" },
];

function statusBadge(c: Customer) {
  if (!c.expireAt) return <Badge variant="neutral">Нет подписки</Badge>;
  const expired = new Date(c.expireAt).getTime() < Date.now();
  if (expired) return <Badge variant="danger">Истекла</Badge>;
  return <Badge variant="success">{c.isTrial ? "Триал" : "Активна"}</Badge>;
}

export function Users() {
  const [page, setPage] = useState<Page<Customer> | null>(null);
  const [pageNum, setPageNum] = useState(1);
  const [filter, setFilter] = useState("");
  const [search, setSearch] = useState("");
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    setLoading(true);
    const params = new URLSearchParams({ page: String(pageNum), limit: "20" });
    if (filter) params.set("filter", filter);
    if (search) params.set("search", search);
    api
      .get<Page<Customer>>(`/admin/api/users?${params.toString()}`)
      .then(setPage)
      .finally(() => setLoading(false));
  }, [pageNum, filter, search]);

  const columns: Column<Customer>[] = [
    { header: "Telegram ID", render: (c) => <TelegramUserLink id={c.telegramId} username={c.username} /> },
    { header: "Статус", render: statusBadge },
    { header: "Истекает", render: (c) => (c.expireAt ? new Date(c.expireAt).toLocaleDateString("ru-RU") : "—") },
    { header: "Язык", render: (c) => c.language },
    { header: "Регистрация", render: (c) => new Date(c.createdAt).toLocaleDateString("ru-RU") },
  ];

  return (
    <div class="stack">
      <div class="row">
        <input
          class="input"
          placeholder="Поиск по Telegram ID…"
          value={search}
          onInput={(e) => {
            setSearch((e.target as HTMLInputElement).value);
            setPageNum(1);
          }}
          style={{ maxWidth: 260 }}
        />
        <select
          class="select"
          value={filter}
          onChange={(e) => {
            setFilter((e.target as HTMLSelectElement).value);
            setPageNum(1);
          }}
        >
          {FILTERS.map((f) => (
            <option key={f.value} value={f.value}>
              {f.label}
            </option>
          ))}
        </select>
      </div>
      <GlassCard>
        {loading && !page ? (
          <div class="shimmer" style={{ height: 200 }} />
        ) : (
          <>
            <DataTable
              columns={columns}
              rows={page?.items ?? []}
              keyFn={(c) => c.id}
              onRowClick={(c) => navigate(`users/${c.telegramId}`)}
              emptyMessage="Пользователи не найдены"
            />
            {page && <Pagination page={pageNum} limit={page.limit} total={page.total} onChange={setPageNum} />}
          </>
        )}
      </GlassCard>
    </div>
  );
}
