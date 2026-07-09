import { useEffect, useState } from "preact/hooks";
import { api } from "../api/client";
import type { Referral, Page } from "../api/types";
import { GlassCard } from "../components/GlassCard";
import { DataTable, type Column } from "../components/DataTable";
import { Pagination } from "../components/Pagination";
import { Badge } from "../components/Badge";

export function Referrals() {
  const [page, setPage] = useState<Page<Referral> | null>(null);
  const [pageNum, setPageNum] = useState(1);

  useEffect(() => {
    api.get<Page<Referral>>(`/admin/api/referrals?page=${pageNum}&limit=25`).then(setPage);
  }, [pageNum]);

  const columns: Column<Referral>[] = [
    { header: "Реферер", render: (r) => <span class="mono">{r.referrerId}</span> },
    { header: "Приглашённый", render: (r) => <span class="mono">{r.refereeId}</span> },
    { header: "Дата", render: (r) => new Date(r.usedAt).toLocaleString("ru-RU") },
    { header: "Бонус", render: (r) => (r.bonusGranted ? <Badge variant="success">Начислен</Badge> : <Badge variant="neutral">Ожидание</Badge>) },
  ];

  return (
    <GlassCard>
      {!page ? (
        <div class="shimmer" style={{ height: 200 }} />
      ) : (
        <>
          <DataTable columns={columns} rows={page.items} keyFn={(r) => r.id} emptyMessage="Рефералов пока нет" />
          <Pagination page={pageNum} limit={page.limit} total={page.total} onChange={setPageNum} />
        </>
      )}
    </GlassCard>
  );
}
