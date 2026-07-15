// Shared glass-styled hover tooltip for ChartBar/ChartLine, replacing ChartBar's native
// (unstyled) <title> tooltip and adding hover feedback to ChartLine (which previously had
// none). Positioned absolutely within a `.chart-wrap` ancestor at pixel coordinates the
// caller computes from the hovered element's own bounding box, so it stays correctly placed
// regardless of how the SVG's viewBox is scaled to its rendered size.
export function ChartTooltip(props: { x: number; y: number; label: string; value: string }) {
  return (
    <div class="chart-tooltip glass-card" style={{ left: `${props.x}px`, top: `${props.y}px` }} role="tooltip">
      <div class="chart-tooltip-label">{props.label}</div>
      <div class="chart-tooltip-value mono">{props.value}</div>
    </div>
  );
}
