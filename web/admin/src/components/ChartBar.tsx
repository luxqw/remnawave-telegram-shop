import type { DayPoint } from "../api/types";

// Bespoke inline-SVG bar chart for daily counters (e.g. new customers/day).
export function ChartBar(props: { points: DayPoint[]; height?: number }) {
  const height = props.height ?? 140;
  const width = 600;
  const max = Math.max(1, ...props.points.map((p) => p.count));
  const gap = 3;
  const barWidth = props.points.length > 0 ? width / props.points.length - gap : width;

  return (
    <svg class="chart-svg" viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none">
      {props.points.map((p, i) => {
        const barHeight = Math.max(2, (p.count / max) * (height - 8));
        const x = i * (barWidth + gap);
        const y = height - barHeight;
        return (
          <rect
            key={p.day}
            class="chart-bar"
            x={x}
            y={y}
            width={Math.max(1, barWidth)}
            height={barHeight}
            rx={2}
          >
            <title>{`${p.day}: ${p.count}`}</title>
          </rect>
        );
      })}
    </svg>
  );
}
