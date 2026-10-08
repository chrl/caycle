# caycle

Free terminal + web app for Tacx smart trainers (tested target: Tacx Vortex Smart).
Talks to the trainer over Bluetooth LE using the Tacx "FE-C over BLE" service,
which carries ANT+ FE-C data pages inside BLE notifications/writes.

## Build

Needs Go and Node.js (for the web dashboard):

```sh
make            # npm build of web/ + go build → ./caycle
make demo       # try it without a bike: simulated trainer on http://localhost:8080
```

On macOS the terminal app needs Bluetooth permission (System Settings →
Privacy & Security → Bluetooth). Close Tacx/Zwift/etc. first — the trainer
accepts only one BLE connection.

## Usage

```sh
caycle scan                 # find trainers and heart rate sensors nearby
caycle inspect              # list the trainer's BLE services
caycle monitor              # print power/cadence/speed once per second (read-only)
caycle ride                 # terminal dashboard + web dashboard, records a ride
caycle ride -route alps.gpx # ride a GPX route with real grades
caycle serve                # web dashboard only; start/finish rides in the browser
caycle rides                # list recorded rides
```

`ride` keys: `↑`/`+` harder, `↓`/`-` easier, `r` resistance (5 % steps),
`e` ERG (10 W steps), `g` grade (0.5 % steps), `m` route (difficulty, 10 %
steps), `q` finish. The trainer reconnects automatically if it drops.

## Web dashboard

`ride` and `serve` start a web dashboard on port 8080 (`-web :9000` to change,
`-web off` to disable). It shows big, readable numbers from across the room,
the route map with an elevation profile, routes and ride history, and works on
a phone on the handlebars: open the printed `http://192.168.x.x:8080` address
on the iPhone (same Wi-Fi) and "Add to Home Screen". macOS may ask once to
allow incoming connections.

Anyone on your network can open it and control the trainer; use
`-web 127.0.0.1:8080` to keep it local to the laptop.

Frontend development: `./caycle serve -demo` in one terminal, `cd web && npm
run dev` in another (Vite proxies `/api` to port 8080).

## Routes

Import a GPX with elevation (export from Strava routes, Komoot, RideWithGPS,
...) in the web dashboard or with `-route file.gpx`. In route mode caycle moves
you along the route by the distance you ride and sets the trainer to the road
grade (smoothed over ±20 m so GPS elevation noise doesn't jerk the brake).
`difficulty` scales the grade, like Zwift's trainer difficulty — handy because
a wheel-on trainer can't simulate steep climbs anyway.

Rides on a route are uploaded to Strava as **Virtual Ride** with the route's
GPS track, so they show a map but stay off real-world segment leaderboards.

## Data

Everything lives in `~/.caycle/`:

- `caycle.db` — SQLite: routes, rides with totals, and one sample per second
  (power, cadence, HR, speed, distance, position, grade, mode, target).
  Samples are written live, so a crash loses nothing — open rides are closed
  on the next start.
- `rides/*.tcx` — exported when a ride is uploaded to Strava.
- `strava.json` — Strava tokens.

```sh
sqlite3 ~/.caycle/caycle.db 'select id, datetime(started_at/1000, "unixepoch", "localtime"), distance_m, avg_power from rides'
```

## Heart rate

`ride` connects in the background to the first standard Bluetooth heart rate
sensor it finds (chest strap, armband, ...). Use `-hr NAME` to pick one or
`-hr off` to disable. Heart rate goes to the dashboards, the database and Strava.

**Apple Watch** doesn't broadcast heart rate over Bluetooth itself. Install a
relay app on the iPhone + Watch (e.g. *HR Broadcast* or *HeartCast*), start
it on both, and the iPhone shows up as a regular heart rate sensor —
`caycle scan` lists it with the `heart-rate` tag.

## Strava

One-time setup:

1. Create an API application at <https://www.strava.com/settings/api>.
   Any name/website is fine; set **Authorization Callback Domain** to `localhost`.
2. Log in with its Client ID and Client Secret — this opens the browser to
   authorize caycle to upload activities:

   ```sh
   caycle strava login -client-id 12345 -client-secret abc...
   ```

   Tokens are stored in `~/.caycle/strava.json` (mode 0600) and refreshed
   automatically.

After that, quitting `ride` asks `upload to Strava? [Y/n]`, and the web
dashboard has an Upload button after finishing a ride and in History. Free
rides upload as indoor (trainer) rides; use `-name "..."` to set the activity
name. To upload an older ride or retry a failed one:

```sh
caycle rides                 # find the ride ID
caycle strava upload 12      # or a .tcx file
caycle strava logout         # forget tokens
```

## ESP32 bike computer

`firmware/` has a standalone version for a Heltec WiFi LoRa 32 (V3): trainer
data on its OLED plus random ERG interval workouts, uploaded to Strava over
Wi-Fi (`caycle device wifi|strava` sets it up over USB). See
[firmware/README.md](firmware/README.md).

## Protocol notes

| What                  | Value                                   |
|-----------------------|-----------------------------------------|
| FE-C service          | `6e40fec1-b5a3-f393-e0a9-e50e24dcca9e`  |
| Trainer → app (notify)| `6e40fec2-…`, frames `A4 09 4E 05 <page> <xor>` |
| App → trainer (write) | `6e40fec3-…`, frames `A4 09 4F 05 <page> <xor>` |

Pages used: `0x10` general (speed, distance, HR), `0x19` trainer (power,
cadence), `0x30` basic resistance, `0x31` target power (ERG), `0x33` grade,
`0x37` user config. See `internal/fec`.

The Vortex is a wheel-on eddy-current trainer: ERG targets are only reachable
within a speed range — the dashboard shows "speed too low/high" when the
trainer reports it can't hold the target.
