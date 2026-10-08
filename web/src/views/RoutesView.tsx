import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { api, errorMessage, type RouteSummary } from "../api";
import { Banner } from "../components/Banner";
import { ElevationSparkline } from "../components/Sparkline";
import { fmtDateTime, fmtInt, fmtKm } from "../format";
import { useRouteDetail } from "../useRouteDetail";

type Props = {
  selectedId: number | null;
  onRide: () => void;
};

export function RoutesView({ selectedId, onRide }: Props) {
  const [routes, setRoutes] = useState<RouteSummary[] | null>(null);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    try {
      setRoutes(await api.routes());
    } catch (err) {
      setError(errorMessage(err));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const ride = async (id: number) => {
    setError("");
    try {
      await api.selectRoute(id);
      onRide();
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  const clear = async () => {
    setError("");
    try {
      await api.selectRoute(null);
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  const remove = async (r: RouteSummary) => {
    if (!window.confirm(`Delete route “${r.name}”?`)) return;
    setError("");
    try {
      await api.deleteRoute(r.id);
      await load();
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  return (
    <div className="page">
      <h1>Routes</h1>
      <UploadForm onUploaded={load} />
      {error && <Banner tone="critical">{error}</Banner>}

      {routes === null ? (
        <p className="muted">Loading routes…</p>
      ) : routes.length === 0 ? (
        <p className="muted">
          No routes yet. Export a GPX (with elevation) from Strava, Komoot or RideWithGPS and upload it above.
        </p>
      ) : (
        <ul className="list">
          {routes.map((r) => (
            <li key={r.id} className={`list-item card ${r.id === selectedId ? "is-selected" : ""}`}>
              <div className="list-main">
                <h2>{r.name}</h2>
                <p className="muted num">
                  {fmtKm(r.distanceM, 1)} km · {fmtInt(r.ascentM)} m ascent · added {fmtDateTime(r.createdAt)}
                </p>
              </div>
              <RouteSparkline id={r.id} />
              <div className="list-actions">
                {r.id === selectedId ? (
                  <>
                    <button type="button" className="btn btn-primary" onClick={onRide}>
                      Selected — go ride
                    </button>
                    <button type="button" className="btn btn-ghost" onClick={() => void clear()}>
                      Unselect
                    </button>
                  </>
                ) : (
                  <button type="button" className="btn btn-primary" onClick={() => void ride(r.id)}>
                    Ride this
                  </button>
                )}
                <button type="button" className="btn btn-ghost btn-danger" onClick={() => void remove(r)}>
                  Delete
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function RouteSparkline({ id }: { id: number }) {
  const detail = useRouteDetail(id);
  return <div className="list-spark">{detail && <ElevationSparkline points={detail.points} />}</div>;
}

function UploadForm({ onUploaded }: { onUploaded: () => Promise<void> }) {
  const fileRef = useRef<HTMLInputElement>(null);
  const [file, setFile] = useState<File | null>(null);
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!file) return;
    setBusy(true);
    setError("");
    try {
      await api.uploadRoute(file, name);
      setFile(null);
      setName("");
      if (fileRef.current) fileRef.current.value = "";
      await onUploaded();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="card upload" onSubmit={submit}>
      <label className="field">
        <span>GPX file</span>
        <input
          ref={fileRef}
          type="file"
          accept=".gpx,application/gpx+xml"
          onChange={(e) => setFile(e.target.files?.[0] ?? null)}
        />
      </label>
      <label className="field">
        <span>Name (optional)</span>
        <input type="text" value={name} placeholder="From the GPX file" onChange={(e) => setName(e.target.value)} />
      </label>
      <button type="submit" className="btn btn-primary" disabled={!file || busy}>
        {busy ? "Uploading…" : "Add route"}
      </button>
      {error && <span className="error-text">{error}</span>}
    </form>
  );
}
