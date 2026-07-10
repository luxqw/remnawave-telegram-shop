import type { ComponentChildren } from "preact";

export interface Column<T> {
  header: string;
  render: (row: T) => ComponentChildren;
  align?: "left" | "right";
}

export function DataTable<T>(props: {
  columns: Column<T>[];
  rows: T[];
  keyFn: (row: T) => string | number;
  onRowClick?: (row: T) => void;
  emptyMessage?: string;
}) {
  if (props.rows.length === 0) {
    return <div class="page-subtitle">{props.emptyMessage ?? "Нет данных"}</div>;
  }
  return (
    <>
      <div class="data-table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              {props.columns.map((col) => (
                <th key={col.header} style={col.align === "right" ? { textAlign: "right" } : undefined}>
                  {col.header}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {props.rows.map((row) => (
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

      {/* Mobile fallback: same data as stacked label:value cards instead of a horizontally
          scrolling table. Rendered unconditionally and toggled via CSS (display:none) rather than
          a JS breakpoint check, so there's no resize-listener or matchMedia bookkeeping. */}
      <div class="data-card-list">
        {props.rows.map((row) => (
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
