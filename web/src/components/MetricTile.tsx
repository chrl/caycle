type Props = {
  label: string;
  value: string;
  unit?: string;
  size?: "hero" | "large" | "small";
  /** Shown under the value, e.g. "avg 182 W". */
  detail?: string;
  /** Dims the value when it is not live (no reading). */
  stale?: boolean;
};

export function MetricTile({ label, value, unit, size = "large", detail, stale }: Props) {
  return (
    <section className={`tile tile-${size}`} aria-label={label}>
      <h2 className="tile-label">{label}</h2>
      <p className={`tile-value ${stale ? "is-stale" : ""}`}>
        <span className="num">{value}</span>
        {unit && <span className="tile-unit">{unit}</span>}
      </p>
      {detail && <p className="tile-detail">{detail}</p>}
    </section>
  );
}
