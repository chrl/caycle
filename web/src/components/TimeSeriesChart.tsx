import { useMemo, type PointerEvent } from "react";
import { fmtDuration } from "../format";
import { bisect, linear, linePath, niceDomain, niceTicks } from "./chartUtils";
import { useSize } from "./useSize";

type Props = {
  title: string;
  unit: string;
  /** Seconds since ride start, ascending. */
  xs: number[];
  /** Values aligned with xs; null = no reading. */
  ys: (number | null)[];
  /** Categorical slot class: "s1" | "s2". */
  series: "s1" | "s2";
  /** Shared crosshair position (seconds), so stacked charts move together. */
  hoverX: number | null;
  onHover: (x: number | null) => void;
  height?: number;
};

const PAD = { top: 10, right: 14, bottom: 24, left: 44 };

/** A single-series line chart over ride time with a shared crosshair. */
export function TimeSeriesChart({ title, unit, xs, ys, series, hoverX, onHover, height = 170 }: Props) {
  const [ref, { width }] = useSize<HTMLDivElement>();
  const plotW = Math.max(0, width - PAD.left - PAD.right);
  const plotH = height - PAD.top - PAD.bottom;
  const maxX = xs[xs.length - 1] ?? 0;

  const values = ys.filter((v): v is number => v !== null);
  const hasData = values.length > 0;
  const yDomain = niceDomain(0, hasData ? Math.max(...values) : 1, 3);
  const x = linear([0, maxX], [PAD.left, PAD.left + plotW]);
  const y = linear(yDomain, [PAD.top + plotH, PAD.top]);
  const baseline = PAD.top + plotH;

  const path = useMemo(() => {
    if (plotW <= 0) return "";
    // Average into one bucket per ~2 px so dense rides stay legible; break the
    // line where recording paused (no samples for more than 10 s).
    const buckets = Math.max(1, Math.floor(plotW / 2));
    const pts: [number, number | null][] = [];
    let i = 0;
    let prevX = -Infinity;
    for (let b = 0; b < buckets && i < xs.length; b++) {
      const end = b === buckets - 1 ? Infinity : ((b + 1) / buckets) * maxX;
      let sum = 0;
      let n = 0;
      const start = i;
      const firstX = xs[i];
      let lastX = firstX;
      for (; i < xs.length && xs[i] <= end; i++) {
        const v = ys[i];
        if (v !== null) {
          sum += v;
          n++;
        }
        lastX = xs[i];
      }
      if (i === start) continue; // empty bucket
      if (firstX - prevX > 10) pts.push([x(firstX), null]);
      pts.push([x(lastX), n ? y(sum / n) : null]);
      prevX = lastX;
    }
    return linePath(pts);
    // x and y are pure functions of xs/ys, plotW and height.
  }, [xs, ys, plotW, height, maxX]);

  const hoverIndex = hoverX !== null && xs.length ? bisect(xs, hoverX) : null;
  const hoverValue = hoverIndex !== null ? ys[hoverIndex] : null;

  const onMove = (e: PointerEvent<SVGSVGElement>) => {
    const rect = e.currentTarget.getBoundingClientRect();
    const px = e.clientX - rect.left;
    onHover(px < PAD.left || px > PAD.left + plotW ? null : ((px - PAD.left) / plotW) * maxX);
  };

  const xTickStep = maxX > 3 * 3600 ? 3600 : maxX > 3600 ? 1800 : maxX > 1200 ? 600 : 300;
  const xTicks: number[] = [];
  for (let t = 0; t <= maxX; t += xTickStep) xTicks.push(t);

  return (
    <figure className="ts-chart">
      <figcaption>
        {title} <span className="muted">({unit})</span>
      </figcaption>
      <div className="chart" ref={ref} style={{ height }}>
        {width > 0 && (
          <svg
            width={width}
            height={height}
            role="img"
            aria-label={`${title} over time${hasData ? `, max ${Math.max(...values)} ${unit}` : ", no data"}`}
            onPointerMove={onMove}
            onPointerLeave={() => onHover(null)}
          >
            {niceTicks(yDomain[0], yDomain[1], 3).map((t) => (
              <g key={t}>
                <line className="grid" x1={PAD.left} x2={PAD.left + plotW} y1={y(t)} y2={y(t)} />
                <text className="tick" x={PAD.left - 8} y={y(t)} dy="0.32em" textAnchor="end">
                  {t.toLocaleString()}
                </text>
              </g>
            ))}
            {xTicks.map((t) => (
              <text key={t} className="tick" x={x(t)} y={baseline + 17} textAnchor="middle">
                {Math.round(t / 60)}′
              </text>
            ))}
            <line className="axis" x1={PAD.left} x2={PAD.left + plotW} y1={baseline} y2={baseline} />
            {hasData ? (
              <path d={path} className={`line ${series}`} />
            ) : (
              <text className="tick" x={PAD.left + plotW / 2} y={PAD.top + plotH / 2} textAnchor="middle">
                No {title.toLowerCase()} recorded
              </text>
            )}
            {hoverIndex !== null && (
              <>
                <line className="crosshair" x1={x(xs[hoverIndex])} x2={x(xs[hoverIndex])} y1={PAD.top} y2={baseline} />
                {hoverValue !== null && (
                  <circle className={`marker ${series}`} cx={x(xs[hoverIndex])} cy={y(hoverValue)} r={5} />
                )}
              </>
            )}
          </svg>
        )}
        {hoverIndex !== null && (
          <div
            className="tooltip"
            style={{ left: Math.min(Math.max(x(xs[hoverIndex]), 64), width - 64), top: 0 }}
          >
            <div className="tooltip-row">
              <span className={`line-key ${series}`} />
              <strong>{hoverValue !== null ? `${Math.round(hoverValue)} ${unit}` : "--"}</strong>
              <span className="muted">{fmtDuration(xs[hoverIndex])}</span>
            </div>
          </div>
        )}
      </div>
    </figure>
  );
}
