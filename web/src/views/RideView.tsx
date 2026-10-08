import { useCallback, useEffect, useState } from "react";
import { api, errorMessage, type Mode, type RideSummary, type Snapshot } from "../api";
import { Banner, type BannerTone } from "../components/Banner";
import { Controls } from "../components/Controls";
import { ElevationProfile } from "../components/ElevationProfile";
import { MetricTile } from "../components/MetricTile";
import { RideSummaryCard } from "../components/RideSummaryCard";
import { RouteMap } from "../components/RouteMap";
import { DASH, fmtDuration, fmtGrade, fmtInt, fmtKm } from "../format";
import type { Live } from "../useLive";
import { useRouteDetail } from "../useRouteDetail";
import { useWakeLock } from "../useWakeLock";

const KEY_MODES: Record<string, Mode> = { r: "resistance", e: "erg", g: "sim", m: "route" };

export function RideView({ live }: { live: Live }) {
  const s = live.snapshot;
  const [summary, setSummary] = useState<RideSummary | null>(null);
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState("");
  const routeDetail = useRouteDetail(s?.route?.id ?? null);

  useWakeLock(true);

  const run = useCallback(async (action: () => Promise<unknown>) => {
    setActionError("");
    try {
      await action();
    } catch (err) {
      setActionError(errorMessage(err));
    }
  }, []);

  const setMode = useCallback((mode: Mode) => run(() => api.control({ mode })), [run]);
  const step = useCallback((dir: 1 | -1) => run(() => api.control({ step: dir })), [run]);

  const start = async () => {
    setBusy(true);
    setSummary(null);
    await run(() => api.startRide());
    setBusy(false);
  };

  const finish = async () => {
    if (!window.confirm("Finish this ride and save it?")) return;
    setBusy(true);
    await run(async () => setSummary(await api.finishRide()));
    setBusy(false);
  };

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const el = e.target as HTMLElement | null;
      if (el && (el.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(el.tagName))) return;
      if (e.key === "ArrowUp" || e.key === "+" || e.key === "=") {
        e.preventDefault();
        void step(1);
      } else if (e.key === "ArrowDown" || e.key === "-" || e.key === "_") {
        e.preventDefault();
        void step(-1);
      } else if (KEY_MODES[e.key.toLowerCase()]) {
        const mode = KEY_MODES[e.key.toLowerCase()];
        if (mode !== "route" || s?.route) void setMode(mode);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [step, setMode, s?.route]);

  if (!s) {
    return (
      <div className="ride-loading">
        {live.connected ? <p>Connecting to caycle…</p> : <Banner tone="critical">Can't reach caycle — is it running?</Banner>}
      </div>
    );
  }

  const route = s.route;
  const hasRoute = route !== null && routeDetail !== null && routeDetail.id === route.id;
  const banners = bannerItems(s, live.connected, actionError);
  const hasTop = banners.length > 0 || summary !== null;

  return (
    <div className={`ride ${hasRoute ? "has-route" : ""} ${hasTop ? "has-top" : ""}`}>
      {hasTop && (
        <div className="ride-top">
          {banners.map((b) => (
            <Banner key={b.text} tone={b.tone}>
              {b.text}
            </Banner>
          ))}
          {summary && (
            <RideSummaryCard ride={summary} stravaConnected={s.strava.connected} onClose={() => setSummary(null)} />
          )}
        </div>
      )}

      <div className="ride-main">
        <div className="metrics">
          <MetricTile
            size="hero"
            label="Power"
            value={fmtInt(s.power)}
            unit="W"
            stale={s.power === null}
          />
          <MetricTile
            label="Heart rate"
            value={fmtInt(s.heartRate)}
            unit="bpm"
            stale={s.heartRate === null}
            detail={s.heartRate === null ? s.hr.status : undefined}
          />
          <MetricTile label="Cadence" value={fmtInt(s.cadence)} unit="rpm" stale={s.cadence === null} />
          <MetricTile label="Speed" value={s.trainer.connected ? s.speedKmh.toFixed(1) : DASH} unit="km/h" />
        </div>

        <div className="secondary">
          <MetricTile size="small" label="Time" value={fmtDuration(s.ride.movingS)} />
          <MetricTile size="small" label="Distance" value={fmtKm(s.ride.distanceM)} unit="km" />
          <MetricTile size="small" label="Avg power" value={fmtInt(s.ride.avgPower)} unit="W" detail={`max ${fmtInt(s.ride.maxPower)} W`} />
          <MetricTile size="small" label="Ascent" value={fmtInt(s.ride.ascentM)} unit="m" />
          <MetricTile size="small" label="Energy" value={fmtInt(s.ride.energyKJ)} unit="kJ" />
        </div>
      </div>

      {hasRoute && route && (
        <section className="ride-route card" aria-label={`Route ${route.name}`}>
          <header className="route-head">
            <div>
              <h2>{route.name}</h2>
              <p className="muted num">
                {fmtKm(Math.max(0, route.distanceM - route.positionM), 1)} km to go · {fmtKm(route.distanceM, 1)} km total
              </p>
            </div>
            <div className="route-grade" aria-label="Current grade">
              <span className="num">{fmtGrade(route.grade)}</span>
              <span className="tile-unit">%</span>
            </div>
          </header>
          <RouteMap
            points={routeDetail.points}
            rider={{ lat: route.lat, lon: route.lon, positionM: route.positionM }}
          />
          <ElevationProfile points={routeDetail.points} positionM={route.positionM} />
        </section>
      )}

      <Controls
        snapshot={s}
        busy={busy}
        onMode={(m) => void setMode(m)}
        onStep={(d) => void step(d)}
        onStart={() => void start()}
        onFinish={() => void finish()}
      />
    </div>
  );
}

type BannerItem = { tone: BannerTone; text: string };

function bannerItems(s: Snapshot, streamUp: boolean, actionError: string): BannerItem[] {
  const items: BannerItem[] = [];
  if (!streamUp) items.push({ tone: "critical", text: "Connection to caycle lost — reconnecting…" });
  if (!s.trainer.connected) items.push({ tone: "warning", text: "Searching for trainer… pedal to wake it up." });
  if (s.control.mode === "erg" && s.trainer.targetStatus)
    items.push({ tone: "warning", text: `ERG: ${s.trainer.targetStatus}` });
  if (s.trainer.writeError) items.push({ tone: "critical", text: s.trainer.writeError });
  if (actionError) items.push({ tone: "critical", text: actionError });
  if (s.route?.done) items.push({ tone: "good", text: "Route complete!" });
  return items;
}
