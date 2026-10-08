import type { Mode } from "./api";

export const DASH = "--";

export function fmtInt(v: number | null | undefined): string {
  return v === null || v === undefined || !Number.isFinite(v) ? DASH : Math.round(v).toString();
}

export function fmtDuration(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  const mmss = `${String(m).padStart(2, "0")}:${String(sec).padStart(2, "0")}`;
  return h > 0 ? `${h}:${mmss}` : mmss;
}

export function fmtKm(meters: number, digits = 2): string {
  return (meters / 1000).toFixed(digits);
}

export function fmtGrade(pct: number): string {
  const v = Math.abs(pct) < 0.05 ? 0 : pct;
  return `${v > 0 ? "+" : v < 0 ? "−" : ""}${Math.abs(v).toFixed(1)}`;
}

export function fmtDateTime(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleString(undefined, {
    weekday: "short",
    day: "numeric",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export const MODE_LABELS: Record<Mode, string> = {
  resistance: "Resistance",
  erg: "ERG",
  sim: "Grade",
  route: "Route",
};
