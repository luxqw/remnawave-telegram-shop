import { useMemo, useRef, useState } from "preact/hooks";
import type { JSX } from "preact";
import type { DayPoint } from "../api/types";
import { ChartTooltip } from "./ChartTooltip";
import { formatDay, pickLabelIndices } from "../lib/chart";

type Hover = { index: number; x: number; y: number };

const AXIS_HEIGHT = 20;
const GRID_FRACTIONS = [0.25, 0.5, 0.75];

// Bespoke inline-SVG bar chart for daily counters (e.g. new customers/day).
export function ChartBar(props: { points: DayPoint[]; height?: number; days?: number }) {
  const height = props.height ?? 140;
  const totalHeight = height + AXIS_HEIGHT;
  const width = 600;
  const max = Math.max(1, ...props.points.map((p) => p.count));
  const gap = 3;
  const barWidth = props.points.length > 0 ? width / props.points.length - gap : width;

  const containerRef = useRef<HTMLDivElement>(null);
  const [hover, setHover] = useState<Hover | null>(null);

  const labelIndices = useMemo(() => pickLabelIndices(props.points.length), [props.points.length]);

  const showHover = (index: number, target: SVGRectElement) => {
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
        {GRID_FRACTIONS.map((f) => (
          <line key={f} class="chart-gridline" x1={0} y1={height * f} x2={width} y2={height * f} />
        ))}
        <line class="chart-axis-line" x1={0} y1={height} x2={width} y2={height} />

        {props.points.map((p, i) => {
          const barHeight = Math.max(2, (p.count / max) * (height - 8));
          const x = i * (barWidth + gap);
          const y = height - barHeight;
          const onEnter = (e: JSX.TargetedEvent<SVGRectElement>) => showHover(i, e.currentTarget);
          return (
            <rect
              key={p.day}
              class={`chart-bar ${hover?.index === i ? "hovered" : ""}`}
              x={x}
              y={y}
              width={Math.max(1, barWidth)}
              height={barHeight}
              rx={2}
              onMouseEnter={onEnter}
              onMouseMove={onEnter}
              onMouseLeave={hideHover}
              onTouchStart={onEnter}
            />
          );
        })}

        {labelIndices.map((i) => {
          const p = props.points[i];
          if (!p) return null;
          const x = i * (barWidth + gap) + Math.max(1, barWidth) / 2;
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
          value={String(props.points[hover.index].count)}
        />
      )}
    </div>
  );
}
