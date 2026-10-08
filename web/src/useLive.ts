import { useEffect, useState } from "react";
import type { Snapshot } from "./api";

export type Live = {
  snapshot: Snapshot | null;
  /** False while the event stream is down; EventSource reconnects on its own. */
  connected: boolean;
};

/** Subscribes to the backend's live Server-Sent Events stream. */
export function useLive(): Live {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [connected, setConnected] = useState(true);

  useEffect(() => {
    const source = new EventSource("/api/live");
    source.onopen = () => setConnected(true);
    source.onerror = () => setConnected(false);
    source.onmessage = (event: MessageEvent<string>) => {
      try {
        setSnapshot(JSON.parse(event.data) as Snapshot);
        setConnected(true);
      } catch {
        // Ignore a malformed frame; the next one replaces it.
      }
    };
    return () => source.close();
  }, []);

  return { snapshot, connected };
}
