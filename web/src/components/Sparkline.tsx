import { useMemo } from "react";
import type { RoutePoint } from "../api";
import { linear, linePath } from "./chartUtils";

/** Tiny elevation silhouette for route lists; the table row carries the numbers. */
export function ElevationSparkline({ points, width = 160, height = 40 }: { points: RoutePoint[]; width?: number; height?: number }) {
  const { line, area } = useMemo(() => {
    if (points.length < 2) return { line: "", area: "" };
    const total = points[points.length - 1][3] || 1;
    const eles = points.map((p) => p[2]);
    const min = Math.min(...eles);
    const max = Math.max(...eles);
    const x = linear([0, total], [1, width - 1]);
    const y = linear([min, max === min ? min + 1 : max], [height - 2, 2]);
    const stride = Math.max(1, Math.floor(points.length / width));
    const pts: [number, number][] = [];
    for (let i = 0; i < points.length; i += stride) pts.push([x(points[i][3]), y(points[i][2])]);
    const line = linePath(pts);
    return { line, area: `${line}L${pts[pts.length - 1][0]},${height}L${pts[0][0]},${height}Z` };
  }, [points, width, height]);

  return (
    <svg className="sparkline" width={width} height={height} viewBox={`0 0 ${width} ${height}`} aria-hidden="true">
      <path d={area} className="area s1" />
      <path d={line} className="line line-thin s1" />
    </svg>
  );
}
