import { useMemo, useState, type PointerEvent } from "react";
import type { RoutePoint } from "../api";
import { fmtGrade } from "../format";
import { bisect, interpolate, linear, linePath, niceDomain, niceTicks } from "./chartUtils";
import { useSize } from "./useSize";

type Props = {
  points: RoutePoint[];
  /** Rider position along the route in meters, or null when not riding it. */
  positionM: number | null;
  height?: number;
};

const PAD = { top: 12, right: 14, bottom: 26, left: 44 };

/** Single-series elevation area chart with the rider's position marked. */
export function ElevationProfile({ points, positionM, height = 180 }: Props) {
  const [ref, { width }] = useSize<HTMLDivElement>();
  const [hoverM, setHoverM] = useState<number | null>(null);

  const data = useMemo(() => {
    const dist = points.map((p) => p[3]);
    const ele = points.map((p) => p[2]);
    const minE = Math.min(...ele);
    const maxE = Math.max(...ele);
    return { dist, ele, total: dist[dist.length - 1] ?? 0, minE, maxE };
  }, [points]);

  const plotW = Math.max(0, width - PAD.left - PAD.right);
  const plotH = height - PAD.top - PAD.bottom;
  const yDomain = niceDomain(data.minE, data.maxE, 3);
  const x = linear([0, data.total], [PAD.left, PAD.left + plotW]);
  const y = linear(yDomain, [PAD.top + plotH, PAD.top]);
  const baseline = PAD.top + plotH;

  const { line, area, doneArea } = useMemo(() => {
    if (plotW <= 0 || data.dist.length < 2) return { line: "", area: "", doneArea: "" };
    // Never draw more vertices than pixels.
    const stride = Math.max(1, Math.floor(data.dist.length / plotW));
    const pts: [number, number][] = [];
    for (let i = 0; i < data.dist.length; i += stride) pts.push([x(data.dist[i]), y(data.ele[i])]);
    const last = data.dist.length - 1;
    if ((last % stride) !== 0) pts.push([x(data.dist[last]), y(data.ele[last])]);
    const line = linePath(pts);
    const area = `${line}L${pts[pts.length - 1][0].toFixed(1)},${baseline}L${pts[0][0].toFixed(1)},${baseline}Z`;
    let doneArea = "";
    if (positionM !== null && positionM > 0) {
      const posX = x(Math.min(positionM, data.total));
      const done = pts.filter(([px]) => px <= posX);
      done.push([posX, y(interpolate(data.dist, data.ele, positionM))]);
      doneArea = `${linePath(done)}L${posX.toFixed(1)},${baseline}L${done[0][0].toFixed(1)},${baseline}Z`;
    }
    return { line, area, doneArea };
    // x and y are pure functions of data, plotW and height.
  }, [data, plotW, height, positionM === null ? null : Math.round(positionM)]);

  const gradeAt = (m: number) => {
    const half = 50;
    const a = Math.max(0, m - half);
    const b = Math.min(data.total, m + half);
    if (b - a < 1) return 0;
    return ((interpolate(data.dist, data.ele, b) - interpolate(data.dist, data.ele, a)) / (b - a)) * 100;
  };

  const onMove = (e: PointerEvent<SVGSVGElement>) => {
    const rect = e.currentTarget.getBoundingClientRect();
    const px = e.clientX - rect.left;
    if (px < PAD.left || px > PAD.left + plotW) {
      setHoverM(null);
      return;
    }
    const m = ((px - PAD.left) / plotW) * data.total;
    // Snap to the nearest route point.
    const i = bisect(data.dist, m);
    const j = Math.min(i + 1, data.dist.length - 1);
    setHoverM(Math.abs(data.dist[j] - m) < Math.abs(data.dist[i] - m) ? data.dist[j] : data.dist[i]);
  };

  const pos = positionM !== null ? Math.min(Math.max(positionM, 0), data.total) : null;
  const hover =
    hoverM !== null
      ? { m: hoverM, ele: interpolate(data.dist, data.ele, hoverM), grade: gradeAt(hoverM) }
      : null;
  const xTicks = niceTicks(0, data.total / 1000, Math.max(2, Math.floor(plotW / 90)));

  return (
    <div className="chart" ref={ref} style={{ height }}>
      {width > 0 && data.dist.length > 1 && (
        <svg
          width={width}
          height={height}
          role="img"
          aria-label={`Elevation profile: ${(data.total / 1000).toFixed(1)} km, ${Math.round(data.minE)} to ${Math.round(data.maxE)} m`}
          onPointerMove={onMove}
          onPointerLeave={() => setHoverM(null)}
        >
          {niceTicks(yDomain[0], yDomain[1], 3).map((t) => (
            <g key={`y${t}`}>
              <line className="grid" x1={PAD.left} x2={PAD.left + plotW} y1={y(t)} y2={y(t)} />
              <text className="tick" x={PAD.left - 8} y={y(t)} dy="0.32em" textAnchor="end">
                {t.toLocaleString()}
              </text>
            </g>
          ))}
          {xTicks.map((t) => (
            <text key={`x${t}`} className="tick" x={x(t * 1000)} y={baseline + 18} textAnchor="middle">
              {t} km
            </text>
          ))}
          <line className="axis" x1={PAD.left} x2={PAD.left + plotW} y1={baseline} y2={baseline} />

          <path d={area} className="area s1" />
          {doneArea && <path d={doneArea} className="area-strong s1" />}
          <path d={line} className="line s1" />

          {pos !== null && (
            <circle className="marker s1" cx={x(pos)} cy={y(interpolate(data.dist, data.ele, pos))} r={7} />
          )}

          {hover && (
            <line className="crosshair" x1={x(hover.m)} x2={x(hover.m)} y1={PAD.top} y2={baseline} />
          )}
        </svg>
      )}
      {hover && (
        <div
          className="tooltip"
          style={{
            left: Math.min(Math.max(x(hover.m), 70), width - 70),
            top: 0,
          }}
        >
          <div className="tooltip-row">
            <span className="line-key s1" />
            <strong>{Math.round(hover.ele)} m</strong>
            <span className="muted">elevation</span>
          </div>
          <div className="tooltip-row">
            <strong>{fmtGrade(hover.grade)} %</strong>
            <span className="muted">at {(hover.m / 1000).toFixed(2)} km</span>
          </div>
        </div>
      )}
    </div>
  );
}
