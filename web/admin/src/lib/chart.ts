// Shared helpers for ChartBar/ChartLine — label thinning + date formatting.

// Picks a thinned-out subset of point indices to label along a chart's x-axis so labels
// don't overlap: at most `maxLabels` evenly-spaced indices, always including index 0 and the
// final index. With the default maxLabels=6 this naturally works out to "every day" for a
// 7-point (7-day) series, "roughly every 5th" for 30, and "roughly every 15th" for 90 —
// matching the 7/30/90-day range selector on the Dashboard.
export function pickLabelIndices(pointCount: number, maxLabels = 6): number[] {
  if (pointCount <= 0) return [];
  if (pointCount <= maxLabels + 1) {
    return Array.from({ length: pointCount }, (_, i) => i);
  }

  const interval = Math.ceil(pointCount / maxLabels);
  const indices: number[] = [];
  for (let i = 0; i < pointCount; i += interval) indices.push(i);
  if (indices[indices.length - 1] !== pointCount - 1) indices.push(pointCount - 1);
  return indices;
}

export function formatDay(day: string): string {
  const d = new Date(day);
  if (Number.isNaN(d.getTime())) return day;
  return d.toLocaleDateString("ru-RU", { day: "2-digit", month: "2-digit" });
}
