import type { DayPoint } from "../api/types";

// Bespoke inline-SVG line chart — no charting library, monochrome per the design system.
export function ChartLine(props: { points: DayPoint[]; height?: number; formatValue?: (v: number) => string }) {
  const height = props.height ?? 220;
  const width = 600; // viewBox width; the SVG scales to its container via CSS.
  const values = props.points.map((p) => p.value);
  const max = Math.max(1, ...values);
  const stepX = props.points.length > 1 ? width / (props.points.length - 1) : width;

  const coords = props.points.map((p, i) => {
    const x = i * stepX;
    const y = height - (p.value / max) * (height - 24) - 12;
    return [x, y] as const;
  });

  const linePath = coords.map(([x, y], i) => `${i === 0 ? "M" : "L"}${x.toFixed(1)},${y.toFixed(1)}`).join(" ");
  const areaPath =
    coords.length > 0
      ? `${linePath} L${coords[coords.length - 1][0].toFixed(1)},${height} L${coords[0][0].toFixed(1)},${height} Z`
      : "";

  const last = props.points[props.points.length - 1];
  const first = props.points[0];

  return (
    <div>
      <svg class="chart-svg" viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none">
        <defs>
          <linearGradient id="chart-fill-gradient" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stop-color="rgba(255,255,255,0.16)" />
            <stop offset="100%" stop-color="rgba(255,255,255,0)" />
          </linearGradient>
        </defs>
        {areaPath && <path class="chart-line-fill" d={areaPath} />}
        {linePath && <path class="chart-line-path" d={linePath} />}
      </svg>
      <div class="row page-subtitle" style={{ justifyContent: "space-between" }}>
        <span>{first ? formatDay(first.day) : ""}</span>
        <span>{last ? formatDay(last.day) : ""}</span>
      </div>
    </div>
  );
}

function formatDay(day: string): string {
  const d = new Date(day);
  if (Number.isNaN(d.getTime())) return day;
  return d.toLocaleDateString("ru-RU", { day: "2-digit", month: "2-digit" });
}
