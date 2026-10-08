// caycle for the Heltec WiFi LoRa 32 (V3): a standalone bike computer for a
// Tacx smart trainer. Shows power, cadence and speed on the OLED and runs
// random ERG interval workouts.
//
// PRG button:
//   short press   start a new random workout / pause / resume
//   double press  skip to the next interval
//   long press    end the workout; when idle, change the workout length
//
// Workouts are recorded to flash and uploaded to Strava over Wi-Fi when they
// end (set up with `caycle device wifi` / `caycle device strava` on the Mac).
//
// Serial console (115200 baud): help, ftp <W>, min <minutes>, bias <+-%>,
// start, stop, skip, plan, status, ssid <name>, pass <password>,
// strava <client id> <client secret> <refresh token>, sync, check.

#include <Preferences.h>

#include "battery.h"
#include "button.h"
#include "display.h"
#include "fec.h"
#include "net.h"
#include "recorder.h"
#include "trainer.h"
#include "workout.h"

namespace {

constexpr int kButtonPin = 0;  // PRG
constexpr float kFreeRideResistance = 10;  // % brake when no workout runs
constexpr uint32_t kResendMs = 5000;
constexpr uint32_t kDrawMs = 100;
constexpr uint32_t kStaleMs = 3000;
constexpr int kLengths[] = {20, 30, 45, 60, 90};

struct Settings {
  int ftp = 200;
  int minutes = 45;
  int bias = 0;  // % added to work blocks
} settings;

Preferences prefs;
Trainer trainer;
Button button;
workout::Player player;

uint32_t lastTick = 0;
uint32_t lastSend = 0;
uint32_t lastDraw = 0;
uint32_t sentEpoch = 0;
bool controlDirty = true;

// Workout totals for the summary.
double energyJ = 0;
uint32_t pedalMs = 0;
uint32_t sampleMs = 0;  // time since the last recorded sample
double distanceM = 0;   // ridden since boot or since the workout started
workout::State lastState = workout::State::Idle;

void saveSettings() {
  prefs.putInt("ftp", settings.ftp);
  prefs.putInt("min", settings.minutes);
  prefs.putInt("bias", settings.bias);
}

void loadSettings() {
  prefs.begin("caycle", false);
  settings.ftp = prefs.getInt("ftp", settings.ftp);
  settings.minutes = prefs.getInt("min", settings.minutes);
  settings.bias = prefs.getInt("bias", settings.bias);
}

void newWorkout() {
  player.load(workout::generate(esp_random(), settings.minutes));
  player.start();
  energyJ = 0;
  pedalMs = 0;
  sampleMs = 0;
  distanceM = 0;
  recorder::start(settings.ftp, player.plan().workCount);
  controlDirty = true;
  Serial.printf("workout: %d min, %d efforts, FTP %d W\n", settings.minutes, player.plan().workCount, settings.ftp);
}

void printPlan() {
  const workout::Plan& p = player.plan();
  for (int i = 0; i < p.count; i++) {
    const workout::Step& s = p.steps[i];
    Serial.printf("%2d %-8s %3d s  %3d %%  %4d W\n", i + 1, workout::kindName(s.kind), s.seconds, s.pct,
                  settings.ftp * (s.pct + (s.kind == workout::Kind::Work ? settings.bias : 0)) / 100);
  }
}

void onPress(Press p) {
  using workout::State;
  const State st = player.state();
  switch (p) {
    case Press::Short:
      if (st == State::Idle || st == State::Done) {
        newWorkout();
      } else {
        player.togglePause();
      }
      break;
    case Press::Double:
      player.skip();
      break;
    case Press::Long:
      if (st == State::Running || st == State::Paused) {
        player.stop();
      } else {
        // Cycle the workout length.
        int next = kLengths[0];
        for (size_t i = 0; i < sizeof(kLengths) / sizeof(kLengths[0]); i++) {
          if (kLengths[i] > settings.minutes) {
            next = kLengths[i];
            break;
          }
        }
        settings.minutes = next;
        saveSettings();
      }
      break;
    case Press::None:
      break;
  }
  controlDirty = true;
}

// Sends the trainer setting for the current state.
void sendControl() {
  fec::Page page;
  switch (player.state()) {
    case workout::State::Running:
      fec::targetPower(player.targetWatts(settings.ftp, settings.bias), page);
      break;
    case workout::State::Paused:
      fec::targetPower(settings.ftp / 2, page);
      break;
    default:
      fec::basicResistance(kFreeRideResistance, page);
      break;
  }
  trainer.send(page);
}

void handleSerial() {
  static String line;
  while (Serial.available()) {
    const char c = static_cast<char>(Serial.read());
    if (c != '\n' && c != '\r') {
      line += c;
      continue;
    }
    line.trim();
    if (line.isEmpty()) continue;
    const int space = line.indexOf(' ');
    const String cmd = space < 0 ? line : line.substring(0, space);
    const String rest = space < 0 ? String() : line.substring(space + 1);
    const int arg = rest.toInt();
    if (cmd == "ssid" && rest.length()) {
      net::setWifiSsid(rest);
      Serial.println("wifi: ssid saved");
    } else if (cmd == "pass") {
      net::setWifiPassword(rest);
      Serial.println("wifi: password saved");
      net::requestSync();
    } else if (cmd == "strava") {
      const int a = rest.indexOf(' '), b = rest.indexOf(' ', a + 1);
      if (a > 0 && b > a) {
        net::setStrava(rest.substring(0, a), rest.substring(a + 1, b), rest.substring(b + 1));
        Serial.println("strava: saved");
        net::requestCheck();
      } else {
        Serial.println("usage: strava <client id> <client secret> <refresh token>");
      }
    } else if (cmd == "sync") {
      net::requestSync();
    } else if (cmd == "check") {
      net::requestCheck();
    } else if (cmd == "ftp" && arg >= 50 && arg <= 600) {
      settings.ftp = arg;
      saveSettings();
    } else if (cmd == "min" && arg >= 20 && arg <= 180) {
      settings.minutes = arg;
      saveSettings();
    } else if (cmd == "bias" && arg >= -30 && arg <= 30) {
      settings.bias = arg;
      saveSettings();
    } else if (cmd == "start") {
      newWorkout();
    } else if (cmd == "stop") {
      player.stop();
    } else if (cmd == "skip") {
      player.skip();
    } else if (cmd == "plan") {
      printPlan();
    } else if (cmd != "status") {
      Serial.println("commands: ftp <W>, min <minutes>, bias <+-%>, start, stop, skip, plan, status,");
      Serial.println("          ssid <name>, pass <password>, strava <id> <secret> <refresh token>, sync, check");
    }
    uint32_t age;
    const fec::Metrics m = trainer.metrics(&age);
    Serial.printf("trainer %s, FTP %d W, workout %d min, bias %+d %%, power %d W, cadence %d rpm, battery %.2f V (%d %%)\n",
                  trainer.status(), settings.ftp, settings.minutes, settings.bias, m.power, m.cadence,
                  battery::volts(), battery::percent());
    Serial.printf("%s%s\n", net::describe().c_str(), net::status().length() ? (", " + net::status()).c_str() : "");
    controlDirty = true;
    line = "";
  }
}

void formatClock(uint32_t ms, char* out, size_t n) {
  const uint32_t s = (ms + 999) / 1000;
  snprintf(out, n, "%lu:%02lu", static_cast<unsigned long>(s / 60), static_cast<unsigned long>(s % 60));
}

void render(const fec::Metrics& m) {
  Screen s;
  s.connected = trainer.connected();
  static char status[32];
  snprintf(status, sizeof(status), "%s Tacx...", trainer.status());
  status[0] = toupper(status[0]);
  s.status = status;
  s.power = m.power;
  s.cadence = m.cadence;
  s.speedKmh = m.speedKmh;
  s.distanceKm = static_cast<float>(distanceM / 1000);
  const String netStatus = net::status();
  if (battery::volts() > 0) snprintf(s.battery, sizeof(s.battery), "%.2fV", battery::volts());

  static char header[24];
  const workout::Step& step = player.step();
  switch (player.state()) {
    case workout::State::Idle:
      snprintf(header, sizeof(header), "FREE  %dmin", settings.minutes);
      snprintf(s.right, sizeof(s.right), "%s", battery::volts() > 0 ? s.battery : "USB");
      s.sideLabel = "FTP";
      snprintf(s.side, sizeof(s.side), "%d", settings.ftp);
      if (net::busy()) s.note = netStatus;
      break;
    case workout::State::Running:
    case workout::State::Paused:
      if (player.state() == workout::State::Paused) {
        snprintf(header, sizeof(header), "PAUSED");
      } else if (step.kind == workout::Kind::Work) {
        snprintf(header, sizeof(header), "WORK %d/%d", player.workNumber(), player.plan().workCount);
      } else {
        snprintf(header, sizeof(header), "%s", workout::kindName(step.kind));
      }
      formatClock(player.stepRemainingMs(), s.right, sizeof(s.right));
      s.progress = player.stepProgress();
      s.sideLabel = "TARGET";
      snprintf(s.side, sizeof(s.side), "%d", player.targetWatts(settings.ftp, settings.bias));
      if (player.state() == workout::State::Running && m.target == fec::TargetStatus::SpeedLow) {
        s.warning = "PEDAL FASTER";
      } else if (player.state() == workout::State::Running && m.target == fec::TargetStatus::SpeedHigh) {
        s.warning = "SHIFT DOWN";
      }
      break;
    case workout::State::Done:
      snprintf(header, sizeof(header), "DONE!");
      formatClock(player.elapsedMs(), s.right, sizeof(s.right));
      s.sideLabel = "AVG W";
      snprintf(s.side, sizeof(s.side), "%d", pedalMs ? static_cast<int>(energyJ * 1000 / pedalMs) : 0);
      s.note = netStatus.length() ? netStatus : String("SAVED");
      break;
  }
  s.header = header;
  display::draw(s);
}

}  // namespace

void setup() {
  Serial.begin(115200);
  pinMode(kButtonPin, INPUT_PULLUP);
  if (!display::begin()) Serial.println("OLED not found");
  display::splash("starting...");
  loadSettings();
  battery::begin();
  if (!recorder::begin()) Serial.println("flash filesystem unavailable; rides won't be saved");
  net::begin();
  player.load(workout::generate(esp_random(), settings.minutes));
  trainer.begin(75, 9, 0.70f);
  lastTick = millis();
  Serial.println("caycle ready; type 'help'");
}

void loop() {
  const uint32_t now = millis();
  const uint32_t dt = now - lastTick;
  lastTick = now;

  trainer.loop();
  battery::loop();
  handleSerial();

  uint32_t age;
  fec::Metrics m = trainer.metrics(&age);
  if (age > kStaleMs) m = fec::Metrics();  // trainer asleep or gone
  const bool pedaling = m.cadence > 0 || m.power > 0;

  const Press p = button.update(digitalRead(kButtonPin) == LOW, now);
  if (p != Press::None) onPress(p);

  player.tick(dt, pedaling);
  if (pedaling && player.state() != workout::State::Paused && player.state() != workout::State::Done) {
    distanceM += m.speedKmh / 3.6 * dt / 1000.0;
  }
  if (player.state() == workout::State::Running && pedaling) {
    pedalMs += dt;
    if (m.power > 0) energyJ += m.power * dt / 1000.0;
    sampleMs += dt;
    if (sampleMs >= 1000) {
      sampleMs -= 1000;
      recorder::sample(m);
    }
  }
  // A workout that ended (finished or stopped) is saved and uploaded.
  const workout::State st = player.state();
  if (st != lastState && (st == workout::State::Done || st == workout::State::Idle) &&
      (lastState == workout::State::Running || lastState == workout::State::Paused)) {
    recorder::finish();
    net::requestSync();
  }
  lastState = st;

  if (trainer.connected()) {
    const bool reconnected = trainer.epoch() != sentEpoch;
    if (player.changed() || controlDirty || reconnected || now - lastSend >= kResendMs) {
      sendControl();
      sentEpoch = trainer.epoch();
      controlDirty = false;
      lastSend = now;
    }
  }

  if (now - lastDraw >= kDrawMs) {
    lastDraw = now;
    render(m);
  }
  delay(5);
}
