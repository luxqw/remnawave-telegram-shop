export function StatTile(props: { label: string; value: string | number; hint?: string }) {
  return (
    <div class="stat-tile">
      <div class="stat-tile-label">{props.label}</div>
      <div class="stat-tile-value mono">{props.value}</div>
      {props.hint && <div class="page-subtitle" style={{ marginTop: 4 }}>{props.hint}</div>}
    </div>
  );
}
