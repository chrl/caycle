# caycle for Heltec WiFi LoRa 32 (V3)

A standalone bike computer for the Tacx trainer: the ESP32 connects to the
Vortex over Bluetooth (FE-C over BLE, same protocol as the Go app), shows
power, cadence and speed on the OLED, and runs random ERG interval workouts.

The trainer accepts one Bluetooth connection: quit `caycle` on the Mac (and
Zwift/Tacx apps) while the board is riding.

## Build and flash

Uses the Arduino IDE's bundled `arduino-cli` with the `esp32` core and the
Adafruit SSD1306 + GFX libraries:

```sh
make -C firmware test     # unit tests for FE-C, workouts and button on the Mac
make -C firmware flash    # compile and upload (board on USB)
make -C firmware monitor  # serial console, 115200 baud
```

Or open `firmware/caycle_esp32/caycle_esp32.ino` in the Arduino IDE with board
"Heltec WiFi LoRa 32(V3)".

## Using it

```
 WORK 3/9         1:23      <- interval and time left in it
 [=========       ]         <- interval progress
 273 W            TARGET    <- your power, ERG target
                  250
 92 rpm       31.4 km/h     <- or a blinking PEDAL FASTER / SHIFT DOWN
```

PRG button:

| Press  | Idle / done                     | During a workout   |
|--------|---------------------------------|--------------------|
| short  | start a new random workout      | pause / resume     |
| double | –                               | skip interval      |
| long   | change length (20/30/45/60/90)  | end the workout    |

Workouts: 6 min warm-up ramp (50/60/70 % FTP), then random efforts — the
shorter, the harder (30 s at 130–150 % … 4 min at 100–110 %) — each followed by
1–4 min recovery at 50–60 %, then 5 min cool-down. The timer only runs while
you pedal. When idle the trainer is set to 10 % resistance.

## Strava sync

Workouts are recorded to the board's flash (one sample per second of pedaling,
~30 KB per hour, room for ~50 hours). When a workout ends — after the
cool-down or with a long press — the board joins your Wi-Fi and uploads it to
Strava as an indoor ride named "Random intervals". The bottom line shows
`UPLOADING...`, then `ON STRAVA`. Rides that couldn't be uploaded (no Wi-Fi)
are retried at the next boot or with `sync`. Workouts under a minute are
discarded.

One-time setup from the Mac, with the board on USB (close the Arduino serial
monitor first):

```sh
caycle device wifi     # asks for the Wi-Fi name and password (2.4 GHz)
caycle device strava   # browser login; the board gets its own Strava authorization
caycle device status
```

The board uses the same Strava API app as caycle on the Mac but its own
authorization, so the two never log each other out. HTTPS is verified against
the root certificates in `certs.h`.

Settings (saved on the board) via the serial console:

```
ftp 230      your FTP in watts (default 200)
min 45       workout length in minutes
bias 5       make work intervals 5 % harder (or -5 easier)
plan         print the current workout
start / stop / skip / status
sync         upload pending rides now
check        test the Strava login
```
