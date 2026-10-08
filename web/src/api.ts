// Types and fetchers for the caycle backend API (same origin).

export type Mode = "resistance" | "erg" | "sim" | "route";

export type Snapshot = {
  trainer: {
    connected: boolean;
    name: string;
    state: string;
    targetStatus: string;
    writeError: string;
  };
  hr: { status: string };
  power: number | null;
  cadence: number | null;
  heartRate: number | null;
  speedKmh: number;
  control: {
    mode: Mode;
    resistance: number;
    power: number;
    grade: number;
    difficulty: number;
    appliedGrade: number;
  };
  ride: {
    recording: boolean;
    id: number;
    movingS: number;
    distanceM: number;
    avgPower: number;
    maxPower: number;
    energyKJ: number;
    ascentM: number;
    avgHeartRate: number;
  };
  route: null | {
    id: number;
    name: string;
    distanceM: number;
    positionM: number;
    lat: number;
    lon: number;
    eleM: number;
    grade: number;
    done: boolean;
  };
  strava: { connected: boolean };
};

export type RouteSummary = {
  id: number;
  name: string;
  distanceM: number;
  ascentM: number;
  createdAt: string;
};

/** [lat, lon, eleM, distM] */
export type RoutePoint = [number, number, number, number];

export type RouteDetail = {
  id: number;
  name: string;
  distanceM: number;
  ascentM: number;
  points: RoutePoint[];
};

export type RideSummary = {
  id: number;
  startedAt: string;
  endedAt: string | null;
  name: string;
  routeName: string | null;
  movingS: number;
  distanceM: number;
  ascentM: number;
  avgPower: number;
  maxPower: number;
  avgHeartRate: number;
  maxHeartRate: number;
  avgCadence: number;
  energyKJ: number;
  stravaUrl: string | null;
};

export type Sample = {
  t: number;
  power: number;
  cadence: number;
  heartRate: number;
  speedKmh: number;
  distanceM: number;
  eleM: number | null;
  grade: number | null;
};

export class ApiError extends Error {
  readonly status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const init: RequestInit = { method, headers: {} };
  if (body instanceof FormData) {
    init.body = body;
  } else if (body !== undefined) {
    init.body = JSON.stringify(body);
    (init.headers as Record<string, string>)["Content-Type"] = "application/json";
  }
  const res = await fetch(path, init);
  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`;
    try {
      const data = (await res.json()) as { error?: string };
      if (data.error) message = data.error;
    } catch {
      // Body was not JSON; keep the status text.
    }
    throw new ApiError(res.status, message);
  }
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  return (text ? JSON.parse(text) : undefined) as T;
}

export const api = {
  control: (body: { mode?: Mode; step?: 1 | -1; value?: number }) =>
    request<void>("POST", "/api/control", body),
  startRide: () => request<{ id: number }>("POST", "/api/ride/start"),
  finishRide: () => request<RideSummary>("POST", "/api/ride/finish"),

  routes: () => request<RouteSummary[]>("GET", "/api/routes"),
  route: (id: number) => request<RouteDetail>("GET", `/api/routes/${id}`),
  uploadRoute: (file: File, name: string) => {
    const form = new FormData();
    form.append("file", file);
    if (name.trim()) form.append("name", name.trim());
    return request<RouteSummary>("POST", "/api/routes", form);
  },
  deleteRoute: (id: number) => request<void>("DELETE", `/api/routes/${id}`),
  selectRoute: (id: number | null) => request<void>("POST", "/api/route/select", { id }),

  rides: () => request<RideSummary[]>("GET", "/api/rides"),
  samples: (id: number) => request<Sample[]>("GET", `/api/rides/${id}/samples`),
  uploadToStrava: (id: number, name?: string) =>
    request<{ url: string }>("POST", `/api/rides/${id}/strava`, name ? { name } : {}),
  deleteRide: (id: number) => request<void>("DELETE", `/api/rides/${id}`),
};

export function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
