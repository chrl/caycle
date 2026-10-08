import { useEffect, useMemo, useState } from "react";
import { CircleMarker, MapContainer, Polyline, TileLayer, useMap } from "react-leaflet";
import type { LatLngBoundsExpression, LatLngExpression } from "leaflet";
import type { RoutePoint } from "../api";
import { bisect } from "./chartUtils";

type Props = {
  points: RoutePoint[];
  /** Rider position; null when the route is selected but not being ridden. */
  rider: { lat: number; lon: number; positionM: number } | null;
};

/** Leaflet map of the route with the ridden part highlighted and a rider marker. */
export function RouteMap({ points, rider }: Props) {
  const [follow, setFollow] = useState(true);

  const latlngs = useMemo<LatLngExpression[]>(() => points.map((p) => [p[0], p[1]]), [points]);
  const bounds = useMemo<LatLngBoundsExpression>(() => {
    let [s, w, n, e] = [90, 180, -90, -180];
    for (const [lat, lon] of points) {
      s = Math.min(s, lat);
      n = Math.max(n, lat);
      w = Math.min(w, lon);
      e = Math.max(e, lon);
    }
    return [
      [s, w],
      [n, e],
    ];
  }, [points]);

  const done = useMemo<LatLngExpression[]>(() => {
    if (!rider || points.length === 0) return [];
    const dist = points.map((p) => p[3]);
    const i = bisect(dist, rider.positionM);
    return [...latlngs.slice(0, i + 1), [rider.lat, rider.lon]];
  }, [points, latlngs, rider?.positionM, rider?.lat, rider?.lon]);

  if (points.length === 0) return null;

  return (
    <div className="map-wrap">
      <MapContainer bounds={bounds} boundsOptions={{ padding: [24, 24] }} className="map" scrollWheelZoom>
        <TileLayer
          attribution='&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors'
          url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png"
          maxZoom={19}
        />
        <Polyline positions={latlngs} pathOptions={{ className: "route-line", weight: 5 }} />
        {done.length > 1 && (
          <Polyline positions={done} pathOptions={{ className: "route-line-done", weight: 5 }} />
        )}
        {rider && (
          <CircleMarker
            center={[rider.lat, rider.lon]}
            radius={9}
            pathOptions={{ className: "rider-marker", weight: 3 }}
          />
        )}
        <FitRoute bounds={bounds} />
        {rider && follow && <Follow lat={rider.lat} lon={rider.lon} />}
      </MapContainer>
      {rider && (
        <button
          type="button"
          className={`map-follow ${follow ? "is-on" : ""}`}
          aria-pressed={follow}
          onClick={() => setFollow((f) => !f)}
        >
          {follow ? "Following" : "Follow rider"}
        </button>
      )}
    </div>
  );
}

/** Refits when a different route is shown in the same map. */
function FitRoute({ bounds }: { bounds: LatLngBoundsExpression }) {
  const map = useMap();
  useEffect(() => {
    map.fitBounds(bounds, { padding: [24, 24] });
  }, [map, bounds]);
  return null;
}

function Follow({ lat, lon }: { lat: number; lon: number }) {
  const map = useMap();
  useEffect(() => {
    const zoom = Math.max(map.getZoom(), 15);
    map.setView([lat, lon], zoom, { animate: true });
  }, [map, lat, lon]);
  return null;
}
