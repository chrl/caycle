import { useState } from "react";
import { api, errorMessage } from "../api";

type Props = {
  rideId: number;
  stravaUrl: string | null;
  connected: boolean;
  onUploaded?: (url: string) => void;
  compact?: boolean;
};

/** Upload-to-Strava action that turns into the activity link once done. */
export function StravaButton({ rideId, stravaUrl, connected, onUploaded, compact }: Props) {
  const [url, setUrl] = useState(stravaUrl);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  if (url) {
    return (
      <a className="strava-link" href={url} target="_blank" rel="noreferrer">
        View on Strava ↗
      </a>
    );
  }
  if (!connected) {
    return compact ? (
      <span className="muted" title="Run `caycle strava login` in the terminal">
        Strava not connected
      </span>
    ) : (
      <p className="muted">
        Connect Strava by running <code>caycle strava login</code> in the terminal.
      </p>
    );
  }

  const upload = async () => {
    setBusy(true);
    setError("");
    try {
      const res = await api.uploadToStrava(rideId);
      setUrl(res.url);
      onUploaded?.(res.url);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <span className="strava-action">
      <button type="button" className={`btn btn-strava ${compact ? "btn-sm" : ""}`} onClick={upload} disabled={busy}>
        {busy ? "Uploading…" : "Upload to Strava"}
      </button>
      {error && <span className="error-text">{error}</span>}
    </span>
  );
}
