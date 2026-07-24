import { useEffect, useState } from "preact/hooks";
import { api, ApiError } from "../api/client";
import type { Referral, Page } from "../api/types";
import { GlassCard } from "../components/GlassCard";
import { DataTable, type Column } from "../components/DataTable";
import { Pagination } from "../components/Pagination";
import { Badge } from "../components/Badge";
import { TelegramUserLink } from "../components/TelegramUserLink";
import { useToast } from "../components/Toast";
import { navigate } from "../router";

export function Referrals() {
  const toast = useToast();
  const [page, setPage] = useState<Page<Referral> | null>(null);
  const [pageNum, setPageNum] = useState(1);

  useEffect(() => {
    api
      .get<Page<Referral>>(`/admin/api/referrals?page=${pageNum}&limit=25`)
      .then(setPage)
      .catch((err) => {
        toast.push(err instanceof ApiError ? err.message : "Не удалось загрузить рефералов", "error");
        setPage({ items: [], total: 0, page: pageNum, limit: 25 });
      });
  }, [pageNum]);

  const columns: Column<Referral>[] = [
    {
      header: "Реферер",
      render: (r) => <TelegramUserLink id={r.referrerId} username={r.referrerUsername} />,
      sortKey: "referrerId",
      sortValue: (r) => r.referrerId,
    },
    {
      header: "Приглашённый",
      render: (r) => <TelegramUserLink id={r.refereeId} username={r.refereeUsername} />,
      sortKey: "refereeId",
      sortValue: (r) => r.refereeId,
    },
    {
      header: "Дата",
      render: (r) => new Date(r.usedAt).toLocaleString("ru-RU"),
      sortKey: "usedAt",
      sortValue: (r) => new Date(r.usedAt).getTime(),
    },
    {
      header: "Бонус",
      render: (r) => (r.bonusGranted ? <Badge variant="success">Начислен</Badge> : <Badge variant="neutral">Ожидание</Badge>),
      sortKey: "bonusGranted",
      sortValue: (r) => (r.bonusGranted ? 1 : 0),
    },
  ];

  return (
    <div class="stack">
      <GlassCard>
        {!page ? (
          <div class="shimmer" style={{ height: 200 }} />
        ) : (
          <>
            <DataTable
              columns={columns}
              rows={page.items}
              keyFn={(r) => r.id}
              onRowClick={(r) => navigate(`users/${r.refereeId}`)}
              emptyMessage="Рефералов пока нет"
            />
            <Pagination page={pageNum} limit={page.limit} total={page.total} onChange={setPageNum} />
          </>
        )}
      </GlassCard>
    </div>
  );
}
