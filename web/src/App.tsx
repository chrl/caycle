import { useCallback, useEffect, useState, type ReactNode } from "react";
import { useLive } from "./useLive";
import { RideView } from "./views/RideView";
import { RoutesView } from "./views/RoutesView";
import { HistoryView } from "./views/HistoryView";
import { FullscreenButton } from "./components/FullscreenButton";

type View = "ride" | "routes" | "history";

const VIEWS: { id: View; label: string; icon: ReactNode }[] = [
  {
    id: "ride",
    label: "Ride",
    icon: (
      <>
        <circle cx="5.5" cy="16.5" r="3.5" />
        <circle cx="18.5" cy="16.5" r="3.5" />
        <path d="M5.5 16.5 9 9h6l3.5 7.5M9 9l3 7.5h6.5M14 5.5h2.5" />
      </>
    ),
  },
  {
    id: "routes",
    label: "Routes",
    icon: (
      <>
        <circle cx="5" cy="18" r="2" />
        <circle cx="19" cy="6" r="2" />
        <path d="M7 18h7a3 3 0 0 0 0-6h-4a3 3 0 0 1 0-6h7" />
      </>
    ),
  },
  {
    id: "history",
    label: "History",
    icon: (
      <>
        <circle cx="12" cy="12" r="9" />
        <path d="M12 7v5l3 2" />
      </>
    ),
  },
];

function viewFromHash(): View {
  const id = window.location.hash.replace("#", "");
  return VIEWS.some((v) => v.id === id) ? (id as View) : "ride";
}

export function App() {
  const live = useLive();
  const [view, setView] = useState<View>(viewFromHash);

  useEffect(() => {
    const onHash = () => setView(viewFromHash());
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);

  const navigate = useCallback((next: View) => {
    if (window.location.hash !== `#${next}`) window.location.hash = next;
    setView(next);
  }, []);

  return (
    <div className="app">
      <header className="topbar">
        <a className="brand" href="#ride" onClick={() => navigate("ride")}>
          <img src="/icon.png" alt="" width={28} height={28} />
          <span>caycle</span>
        </a>
        <nav className="tabs" aria-label="Views">
          {VIEWS.map((v) => (
            <button
              key={v.id}
              type="button"
              className="tab"
              aria-current={view === v.id ? "page" : undefined}
              onClick={() => navigate(v.id)}
            >
              <svg viewBox="0 0 24 24" aria-hidden="true">
                {v.icon}
              </svg>
              <span>{v.label}</span>
            </button>
          ))}
        </nav>
        <div className="topbar-actions">
          <TrainerChip live={live} />
          <FullscreenButton />
        </div>
      </header>

      <main className="content">
        {view === "ride" && <RideView live={live} />}
        {view === "routes" && (
          <RoutesView selectedId={live.snapshot?.route?.id ?? null} onRide={() => navigate("ride")} />
        )}
        {view === "history" && <HistoryView stravaConnected={live.snapshot?.strava.connected ?? false} />}
      </main>
    </div>
  );
}

function TrainerChip({ live }: { live: ReturnType<typeof useLive> }) {
  const t = live.snapshot?.trainer;
  const ok = live.connected && t?.connected;
  const label = !live.connected ? "Offline" : t?.connected ? t.name || "Trainer" : "Searching…";
  return (
    <div className={`chip ${ok ? "chip-ok" : "chip-warn"}`} title={label}>
      <span className="chip-dot" aria-hidden="true" />
      <span className="chip-label">{label}</span>
    </div>
  );
}
