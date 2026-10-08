import type { ReactNode } from "react";

export type BannerTone = "info" | "warning" | "critical" | "good";

const ICONS: Record<BannerTone, ReactNode> = {
  info: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 11v5M12 8h.01" />
    </>
  ),
  warning: <path d="M12 4 2.5 20h19L12 4ZM12 10v4M12 17h.01" />,
  critical: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="m9 9 6 6M15 9l-6 6" />
    </>
  ),
  good: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="m8 12 3 3 5-6" />
    </>
  ),
};

/** Status message; tone is carried by icon + label, never color alone. */
export function Banner({ tone, children }: { tone: BannerTone; children: ReactNode }) {
  return (
    <div className={`banner banner-${tone}`} role={tone === "critical" ? "alert" : "status"}>
      <svg viewBox="0 0 24 24" aria-hidden="true" className="banner-icon">
        {ICONS[tone]}
      </svg>
      <span>{children}</span>
    </div>
  );
}
