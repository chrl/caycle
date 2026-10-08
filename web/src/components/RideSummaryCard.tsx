import type { RideSummary } from "../api";
import { fmtDuration, fmtInt, fmtKm } from "../format";
import { StravaButton } from "./StravaButton";

type Props = {
  ride: RideSummary;
  stravaConnected: boolean;
  onClose: () => void;
};

/** Shown after finishing a ride: totals plus the Strava upload action. */
export function RideSummaryCard({ ride, stravaConnected, onClose }: Props) {
  const stats: [string, string][] = [
    ["Moving time", fmtDuration(ride.movingS)],
    ["Distance", `${fmtKm(ride.distanceM)} km`],
    ["Avg power", `${fmtInt(ride.avgPower)} W`],
    ["Max power", `${fmtInt(ride.maxPower)} W`],
    ["Avg heart rate", ride.avgHeartRate > 0 ? `${fmtInt(ride.avgHeartRate)} bpm` : "--"],
    ["Ascent", `${fmtInt(ride.ascentM)} m`],
    ["Energy", `${fmtInt(ride.energyKJ)} kJ`],
  ];
  return (
    <section className="card summary" aria-labelledby="summary-title">
      <header className="card-head">
        <h2 id="summary-title">Ride complete{ride.routeName ? ` · ${ride.routeName}` : ""}</h2>
        <button type="button" className="btn btn-ghost btn-sm" onClick={onClose} aria-label="Close summary">
          Close
        </button>
      </header>
      <dl className="summary-grid">
        {stats.map(([label, value]) => (
          <div key={label}>
            <dt>{label}</dt>
            <dd className="num">{value}</dd>
          </div>
        ))}
      </dl>
      <StravaButton rideId={ride.id} stravaUrl={ride.stravaUrl} connected={stravaConnected} />
    </section>
  );
}
