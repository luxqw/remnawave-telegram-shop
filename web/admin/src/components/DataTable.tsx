import type { ComponentChildren } from "preact";
import { useMemo, useState } from "preact/hooks";

export type Column<T> = {
  header: string;
  render: (row: T) => ComponentChildren;
  align?: "left" | "right";
  /** Omit to make the column non-sortable. */
  sortKey?: string;
  /** Required when sortKey is set — the raw value to compare (not the rendered JSX). */
  sortValue?: (row: T) => string | number | null;
};

type SortState = { key: string; dir: "asc" | "desc" } | null;

// compareBySortValue keeps nulls last regardless of direction: dir only flips the ordering of two
// non-null values, so a descending sort still pushes unknowns to the bottom instead of the top.
function compareBySortValue<T>(a: T, b: T, sortValue: (row: T) => string | number | null, dirMul: 1 | -1): number {
  const av = sortValue(a);
  const bv = sortValue(b);
  if (av === null && bv === null) return 0;
  if (av === null) return 1;
  if (bv === null) return -1;
  if (typeof av === "number" && typeof bv === "number") return dirMul * (av - bv);
  return dirMul * String(av).localeCompare(String(bv), "ru");
}

export function DataTable<T>(props: {
  columns: Column<T>[];
  rows: T[];
  keyFn: (row: T) => string | number;
  onRowClick?: (row: T) => void;
  emptyMessage?: string;
}) {
  const [sort, setSort] = useState<SortState>(null);
  const sortColumn = sort ? props.columns.find((c) => c.sortKey === sort.key) : undefined;
  const sortableColumns = props.columns.filter((c) => c.sortKey);

  const sortedRows = useMemo(() => {
    if (!sort || !sortColumn?.sortValue) return props.rows;
    const dirMul = sort.dir === "asc" ? 1 : -1;
    const sortValue = sortColumn.sortValue;
    return [...props.rows].sort((a, b) => compareBySortValue(a, b, sortValue, dirMul));
  }, [props.rows, sort, sortColumn]);

  // asc -> desc -> null (unsorted). Picking a different sortable column always starts at asc,
  // discarding whatever sort was previously active — single-column sort only.
  const cycleSort = (col: Column<T>) => {
    if (!col.sortKey) return;
    setSort((prev) => {
      if (!prev || prev.key !== col.sortKey) return { key: col.sortKey!, dir: "asc" };
      if (prev.dir === "asc") return { key: col.sortKey!, dir: "desc" };
      return null;
    });
  };

  if (props.rows.length === 0) {
    return <div class="page-subtitle">{props.emptyMessage ?? "Нет данных"}</div>;
  }

  return (
    <>
      <div class="data-table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              {props.columns.map((col) => {
                const active = sort?.key === col.sortKey;
                return (
                  <th
                    key={col.header}
                    class={col.sortKey ? "sortable" : undefined}
                    style={col.align === "right" ? { textAlign: "right" } : undefined}
                    onClick={col.sortKey ? () => cycleSort(col) : undefined}
                  >
                    {col.header}
                    {col.sortKey && (
                      <span class={`sort-indicator ${active ? "active" : ""}`}>
                        {active ? (sort!.dir === "asc" ? " ▲" : " ▼") : " ⇅"}
                      </span>
                    )}
                  </th>
                );
              })}
            </tr>
          </thead>
          <tbody>
            {sortedRows.map((row) => (
              <tr key={props.keyFn(row)} onClick={() => props.onRowClick?.(row)}>
                {props.columns.map((col) => (
                  <td key={col.header} class={col.align === "right" ? "num" : undefined}>
                    {col.render(row)}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Mobile fallback: same sorted data as stacked label:value cards instead of a horizontally
          scrolling table. Rendered unconditionally and toggled via CSS (display:none) rather than
          a JS breakpoint check, so there's no resize-listener or matchMedia bookkeeping. */}
      <div class="data-card-list">
        {sortableColumns.length > 0 && (
          <select
            class="select mobile-sort-select"
            aria-label="Сортировка"
            value={sort ? `${sort.key}:${sort.dir}` : ""}
            onChange={(e) => {
              const v = (e.target as HTMLSelectElement).value;
              if (!v) {
                setSort(null);
                return;
              }
              const sep = v.lastIndexOf(":");
              const key = v.slice(0, sep);
              const dir = v.slice(sep + 1) as "asc" | "desc";
              setSort({ key, dir });
            }}
          >
            <option value="">Сортировка: по умолчанию</option>
            {sortableColumns.flatMap((col) => [
              <option key={`${col.sortKey}-asc`} value={`${col.sortKey}:asc`}>
                {col.header} ↑
              </option>,
              <option key={`${col.sortKey}-desc`} value={`${col.sortKey}:desc`}>
                {col.header} ↓
              </option>,
            ])}
          </select>
        )}
        {sortedRows.map((row) => (
          <div class="data-card" key={props.keyFn(row)} onClick={() => props.onRowClick?.(row)}>
            {props.columns.map((col) => (
              <div class="data-card-row" key={col.header}>
                <span class="data-card-label">{col.header}</span>
                <span class="data-card-value">{col.render(row)}</span>
              </div>
            ))}
          </div>
        ))}
      </div>
    </>
  );
}
