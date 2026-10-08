import { useEffect, useState } from "react";
import { api, type RouteDetail } from "./api";

const cache = new Map<number, RouteDetail>();

/** Loads (and caches) a route's points by id; null id clears it. */
export function useRouteDetail(id: number | null): RouteDetail | null {
  const [detail, setDetail] = useState<RouteDetail | null>(id !== null ? cache.get(id) ?? null : null);

  useEffect(() => {
    if (id === null) {
      setDetail(null);
      return;
    }
    const cached = cache.get(id);
    if (cached) {
      setDetail(cached);
      return;
    }
    let cancelled = false;
    api
      .route(id)
      .then((r) => {
        cache.set(id, r);
        if (!cancelled) setDetail(r);
      })
      .catch(() => {
        if (!cancelled) setDetail(null);
      });
    return () => {
      cancelled = true;
    };
  }, [id]);

  return detail;
}
