import { useCallback, useEffect, useMemo, useState } from "react";
import { api, errorMessage, type RideSummary, type Sample } from "../api";
import { Banner } from "../components/Banner";
import { StravaButton } from "../components/StravaButton";
import { TimeSeriesChart } from "../components/TimeSeriesChart";
import { fmtDateTime, fmtDuration, fmtInt, fmtKm } from "../format";

export function HistoryView({ stravaConnected }: { stravaConnected: boolean }) {
  const [rides, setRides] = useState<RideSummary[] | null>(null);
  const [openId, setOpenId] = useState<number | null>(null);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    try {
      const list = await api.rides();
      list.sort((a, b) => b.startedAt.localeCompare(a.startedAt));
      setRides(list);
    } catch (err) {
      setError(errorMessage(err));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const remove = async (r: RideSummary) => {
    if (!window.confirm(`Delete the ride from ${fmtDateTime(r.startedAt)}? This cannot be undone.`)) return;
    setError("");
    try {
      await api.deleteRide(r.id);
      setOpenId(null);
      await load();
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  return (
    <div className="page">
      <h1>History</h1>
      {error && <Banner tone="critical">{error}</Banner>}
      {rides === null ? (
        <p className="muted">Loading rides…</p>
      ) : rides.length === 0 ? (
        <p className="muted">No rides yet — go pedal!</p>
      ) : (
        <ul className="list">
          {rides.map((r) => {
            const open = openId === r.id;
            return (
              <li key={r.id} className={`list-item card ride-item ${open ? "is-open" : ""}`}>
                <button
                  type="button"
                  className="ride-row"
                  aria-expanded={open}
                  onClick={() => setOpenId(open ? null : r.id)}
                >
                  <span className="ride-title">
                    <strong>{fmtDateTime(r.startedAt)}</strong>
                    <span className="muted">{r.routeName ?? r.name ?? ""}</span>
                    {r.endedAt === null && <span className="badge">in progress</span>}
                  </span>
                  <dl className="ride-stats">
                    <Stat label="Time" value={fmtDuration(r.movingS)} />
                    <Stat label="Distance" value={`${fmtKm(r.distanceM, 1)} km`} />
                    <Stat label="Avg / max" value={`${fmtInt(r.avgPower)} / ${fmtInt(r.maxPower)} W`} />
                    <Stat label="Avg HR" value={r.avgHeartRate > 0 ? `${fmtInt(r.avgHeartRate)} bpm` : "--"} />
                    <Stat label="Energy" value={`${fmtInt(r.energyKJ)} kJ`} />
                  </dl>
                </button>
                <div className="ride-actions">
                  <StravaButton
                    rideId={r.id}
                    stravaUrl={r.stravaUrl}
                    connected={stravaConnected}
                    compact
                    onUploaded={(url) =>
                      setRides((list) => list?.map((x) => (x.id === r.id ? { ...x, stravaUrl: url } : x)) ?? null)
                    }
                  />
                </div>
                {open && <RideDetail ride={r} onDelete={() => void remove(r)} />}
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd className="num">{value}</dd>
    </div>
  );
}

function RideDetail({ ride, onDelete }: { ride: RideSummary; onDelete: () => void }) {
  const [samples, setSamples] = useState<Sample[] | null>(null);
  const [error, setError] = useState("");
  const [hoverX, setHoverX] = useState<number | null>(null);

  useEffect(() => {
    let cancelled = false;
    api
      .samples(ride.id)
      .then((s) => !cancelled && setSamples(s))
      .catch((err) => !cancelled && setError(errorMessage(err)));
    return () => {
      cancelled = true;
    };
  }, [ride.id]);

  const series = useMemo(() => {
    if (!samples || samples.length === 0) return null;
    const t0 = samples[0].t;
    return {
      xs: samples.map((s) => (s.t - t0) / 1000),
      power: samples.map((s) => (s.power >= 0 ? s.power : null)),
      hr: samples.map((s) => (s.heartRate > 0 ? s.heartRate : null)),
    };
  }, [samples]);

  return (
    <div className="ride-detail">
      {error && <Banner tone="critical">{error}</Banner>}
      {!samples && !error && <p className="muted">Loading samples…</p>}
      {samples && !series && <p className="muted">This ride has no recorded samples.</p>}
      {series && (
        <div className="charts">
          <TimeSeriesChart
            title="Power"
            unit="W"
            xs={series.xs}
            ys={series.power}
            series="s1"
            hoverX={hoverX}
            onHover={setHoverX}
          />
          <TimeSeriesChart
            title="Heart rate"
            unit="bpm"
            xs={series.xs}
            ys={series.hr}
            series="s2"
            hoverX={hoverX}
            onHover={setHoverX}
          />
        </div>
      )}
      <dl className="summary-grid">
        <Stat label="Avg cadence" value={`${fmtInt(ride.avgCadence)} rpm`} />
        <Stat label="Max HR" value={ride.maxHeartRate > 0 ? `${fmtInt(ride.maxHeartRate)} bpm` : "--"} />
        <Stat label="Ascent" value={`${fmtInt(ride.ascentM)} m`} />
      </dl>
      <div className="detail-actions">
        <button type="button" className="btn btn-ghost btn-danger" onClick={onDelete}>
          Delete ride
        </button>
      </div>
    </div>
  );
}
