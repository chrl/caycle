import { useCallback, useEffect, useState } from "react";

// Safari (macOS/iPadOS) still only ships the prefixed API.
type WebkitDocument = Document & {
  webkitFullscreenEnabled?: boolean;
  webkitFullscreenElement?: Element | null;
  webkitExitFullscreen?: () => Promise<void> | void;
};
type WebkitElement = HTMLElement & { webkitRequestFullscreen?: () => Promise<void> | void };

const doc = document as WebkitDocument;

function supported(): boolean {
  return Boolean(doc.fullscreenEnabled || doc.webkitFullscreenEnabled);
}

function isFullscreen(): boolean {
  return Boolean(doc.fullscreenElement || doc.webkitFullscreenElement);
}

async function toggle(): Promise<void> {
  try {
    if (isFullscreen()) {
      await (doc.exitFullscreen ? doc.exitFullscreen() : doc.webkitExitFullscreen?.());
    } else {
      const el = document.documentElement as WebkitElement;
      await (el.requestFullscreen ? el.requestFullscreen() : el.webkitRequestFullscreen?.());
    }
  } catch {
    // Denied (e.g. not triggered by a user gesture); nothing useful to show.
  }
}

function isTyping(target: EventTarget | null): boolean {
  return target instanceof HTMLElement && (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName));
}

/**
 * Toggles fullscreen for the whole app (also with the "f" key). Hidden where
 * the browser can't do it, e.g. iPhone Safari — there, "Add to Home Screen"
 * gives a chrome-less app instead.
 */
export function FullscreenButton() {
  const [active, setActive] = useState(isFullscreen);
  const onClick = useCallback(() => void toggle(), []);

  useEffect(() => {
    const onChange = () => setActive(isFullscreen());
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "f" && !e.metaKey && !e.ctrlKey && !e.altKey && !isTyping(e.target)) void toggle();
    };
    document.addEventListener("fullscreenchange", onChange);
    document.addEventListener("webkitfullscreenchange", onChange);
    window.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("fullscreenchange", onChange);
      document.removeEventListener("webkitfullscreenchange", onChange);
      window.removeEventListener("keydown", onKey);
    };
  }, []);

  if (!supported()) return null;

  const label = active ? "Exit fullscreen (f)" : "Fullscreen (f)";
  return (
    <button type="button" className="icon-button" onClick={onClick} title={label} aria-label={label} aria-pressed={active}>
      <svg viewBox="0 0 24 24" aria-hidden="true">
        {active ? (
          <path d="M9 4v5H4M15 4v5h5M9 20v-5H4M15 20v-5h5" />
        ) : (
          <path d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5" />
        )}
      </svg>
    </button>
  );
}
