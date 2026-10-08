// Small helpers shared by the SVG charts.

export type Scale = (v: number) => number;

export function linear(domain: [number, number], range: [number, number]): Scale {
  const [d0, d1] = domain;
  const [r0, r1] = range;
  const span = d1 - d0 || 1;
  return (v) => r0 + ((v - d0) / span) * (r1 - r0);
}

/** Clean tick values (1/2/5 × 10^n steps) covering [min, max]. */
export function niceTicks(min: number, max: number, count = 4): number[] {
  if (!Number.isFinite(min) || !Number.isFinite(max)) return [];
  if (min === max) return [min];
  const raw = (max - min) / Math.max(1, count);
  const mag = 10 ** Math.floor(Math.log10(raw));
  const step = [1, 2, 5, 10].map((m) => m * mag).find((s) => s >= raw) ?? raw;
  const ticks: number[] = [];
  for (let v = Math.ceil(min / step) * step; v <= max + step * 1e-9; v += step) {
    ticks.push(Math.round(v / step) * step);
  }
  return ticks;
}

/** Expands [min, max] outward to tick boundaries so marks never touch the frame. */
export function niceDomain(min: number, max: number, count = 4): [number, number] {
  if (min === max) return [min - 1, max + 1];
  const ticks = niceTicks(min, max, count);
  const step = ticks.length > 1 ? ticks[1] - ticks[0] : 1;
  return [Math.floor(min / step) * step, Math.ceil(max / step) * step];
}

/** Index of the last element in a sorted array whose value is <= x. */
export function bisect(sorted: ArrayLike<number>, x: number): number {
  let lo = 0;
  let hi = sorted.length - 1;
  if (hi < 0 || x <= sorted[0]) return 0;
  if (x >= sorted[hi]) return hi;
  while (hi - lo > 1) {
    const mid = (lo + hi) >> 1;
    if (sorted[mid] <= x) lo = mid;
    else hi = mid;
  }
  return lo;
}

/** Linear interpolation of ys at x over sorted xs. */
export function interpolate(xs: ArrayLike<number>, ys: ArrayLike<number>, x: number): number {
  if (xs.length === 0) return NaN;
  const i = bisect(xs, x);
  if (i >= xs.length - 1) return ys[xs.length - 1];
  const x0 = xs[i];
  const x1 = xs[i + 1];
  if (x <= x0 || x1 === x0) return ys[i];
  return ys[i] + ((x - x0) / (x1 - x0)) * (ys[i + 1] - ys[i]);
}

/** Builds an SVG path through points; `null` y values break the line. */
export function linePath(points: [number, number | null][]): string {
  let d = "";
  let pen = false;
  for (const [x, y] of points) {
    if (y === null || !Number.isFinite(y)) {
      pen = false;
      continue;
    }
    d += `${pen ? "L" : "M"}${x.toFixed(1)},${y.toFixed(1)}`;
    pen = true;
  }
  return d;
}
