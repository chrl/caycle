import type { Mode, Snapshot } from "../api";
import { fmtGrade, MODE_LABELS } from "../format";

type Props = {
  snapshot: Snapshot;
  busy: boolean;
  onMode: (mode: Mode) => void;
  onStep: (step: 1 | -1) => void;
  onStart: () => void;
  onFinish: () => void;
};

const MODES: Mode[] = ["resistance", "erg", "sim", "route"];

function target(s: Snapshot): { label: string; value: string; unit: string; detail?: string } {
  const c = s.control;
  switch (c.mode) {
    case "erg":
      return { label: "Target power", value: Math.round(c.power).toString(), unit: "W" };
    case "sim":
      return { label: "Grade", value: fmtGrade(c.grade), unit: "%" };
    case "route":
      return {
        label: "Difficulty",
        value: Math.round(c.difficulty).toString(),
        unit: "%",
        detail: `grade ${fmtGrade(c.appliedGrade)} %`,
      };
    default:
      return { label: "Resistance", value: Math.round(c.resistance).toString(), unit: "%" };
  }
}

/** Mode selector, big −/+ target adjusters and the start/finish button. */
export function Controls({ snapshot, busy, onMode, onStep, onStart, onFinish }: Props) {
  const t = target(snapshot);
  const mode = snapshot.control.mode;

  return (
    <div className="controls">
      <div className="segmented" role="radiogroup" aria-label="Mode">
        {MODES.map((m) => {
          const disabled = m === "route" && !snapshot.route;
          return (
            <button
              key={m}
              type="button"
              role="radio"
              aria-checked={mode === m}
              disabled={disabled}
              title={disabled ? "Pick a route on the Routes tab first" : undefined}
              onClick={() => onMode(m)}
            >
              <span className="label-full">{MODE_LABELS[m]}</span>
              <span className="label-short" aria-hidden="true">
                {m === "resistance" ? "Resist." : MODE_LABELS[m]}
              </span>
            </button>
          );
        })}
      </div>

      <div className="adjust">
        <button type="button" className="btn-step" aria-label={`Decrease ${t.label.toLowerCase()}`} onClick={() => onStep(-1)}>
          −
        </button>
        <div className="target" aria-live="polite">
          <span className="target-label">{t.label}</span>
          <span className="target-value">
            <span className="num">{t.value}</span>
            <span className="target-unit">{t.unit}</span>
          </span>
          {t.detail && <span className="target-detail">{t.detail}</span>}
        </div>
        <button type="button" className="btn-step" aria-label={`Increase ${t.label.toLowerCase()}`} onClick={() => onStep(1)}>
          +
        </button>
      </div>

      {snapshot.ride.recording ? (
        <button type="button" className="btn btn-finish" onClick={onFinish} disabled={busy}>
          Finish ride
        </button>
      ) : (
        <button type="button" className="btn btn-start" onClick={onStart} disabled={busy}>
          Start ride
        </button>
      )}
    </div>
  );
}
