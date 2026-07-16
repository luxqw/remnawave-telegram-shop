import { useMemo, useRef, useState } from "preact/hooks";
import type { JSX } from "preact";
import type { DayPoint } from "../api/types";
import { ChartTooltip } from "./ChartTooltip";
import { formatDay, pickLabelIndices } from "../lib/chart";

type Hover = { index: number; x: number; y: number };

const AXIS_HEIGHT = 20;
const GRID_FRACTIONS = [0.25, 0.5, 0.75];
const HIT_RADIUS = 10;
const DOT_RADIUS = 3;

// Bespoke inline-SVG line chart — no charting library, monochrome per the design system.
export function ChartLine(props: {
  points: DayPoint[];
  height?: number;
  formatValue?: (v: number) => string;
}) {
  const height = props.height ?? 220;
  const totalHeight = height + AXIS_HEIGHT;
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

  const containerRef = useRef<HTMLDivElement>(null);
  const [hover, setHover] = useState<Hover | null>(null);

  const labelIndices = useMemo(() => pickLabelIndices(props.points.length), [props.points.length]);
  const formatValue = props.formatValue ?? ((v: number) => String(v));

  const showHover = (index: number, target: SVGCircleElement) => {
    const container = containerRef.current;
    if (!container) return;
    const rect = target.getBoundingClientRect();
    const containerRect = container.getBoundingClientRect();
    setHover({ index, x: rect.left + rect.width / 2 - containerRect.left, y: rect.top - containerRect.top });
  };

  const hideHover = () => setHover(null);

  return (
    <div class="chart-wrap" ref={containerRef}>
      <svg class="chart-svg" viewBox={`0 0 ${width} ${totalHeight}`} preserveAspectRatio="none">
        <defs>
          <linearGradient id="chart-fill-gradient" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" style={{ stopColor: "var(--chart-fill-strong)" }} />
            <stop offset="100%" style={{ stopColor: "var(--chart-fill-transparent)" }} />
          </linearGradient>
        </defs>

        {GRID_FRACTIONS.map((f) => (
          <line key={f} class="chart-gridline" x1={0} y1={height * f} x2={width} y2={height * f} />
        ))}
        <line class="chart-axis-line" x1={0} y1={height} x2={width} y2={height} />

        {areaPath && <path class="chart-line-fill" d={areaPath} />}
        {linePath && <path class="chart-line-path" d={linePath} />}

        {coords.map(([x, y], i) => {
          const p = props.points[i];
          const onEnter = (e: JSX.TargetedEvent<SVGCircleElement>) => showHover(i, e.currentTarget);
          return (
            <g key={p.day}>
              <circle class={`chart-point-dot ${hover?.index === i ? "visible" : ""}`} cx={x} cy={y} r={DOT_RADIUS} />
              <circle
                class="chart-point-hit"
                cx={x}
                cy={y}
                r={HIT_RADIUS}
                onMouseEnter={onEnter}
                onMouseMove={onEnter}
                onMouseLeave={hideHover}
                onTouchStart={onEnter}
              />
            </g>
          );
        })}

        {labelIndices.map((i) => {
          const p = props.points[i];
          if (!p) return null;
          const x = coords[i][0];
          return (
            <g key={p.day}>
              <line class="chart-axis-tick" x1={x} y1={height} x2={x} y2={height + 4} />
              <text class="chart-axis-label" x={x} y={height + 15} text-anchor="middle">
                {formatDay(p.day)}
              </text>
            </g>
          );
        })}
      </svg>
      {hover && props.points[hover.index] && (
        <ChartTooltip
          x={hover.x}
          y={hover.y}
          label={formatDay(props.points[hover.index].day)}
          value={formatValue(props.points[hover.index].value)}
        />
      )}
    </div>
  );
}
