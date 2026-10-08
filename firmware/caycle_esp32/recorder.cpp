#include "recorder.h"

#include <LittleFS.h>
#include <Preferences.h>
#include <time.h>

namespace recorder {
namespace {

constexpr const char* kDir = "/rides";
constexpr uint32_t kMinSamples = 60;
constexpr uint32_t kFlushEvery = 10;
constexpr time_t kValidEpoch = 1700000000;  // the clock is synced once it's past 2023

SemaphoreHandle_t lock = nullptr;
fs::File current;
String currentPath;
uint32_t startMillis = 0;
uint32_t samples = 0;

// Rides finished this boot before the clock was synced: path and how long
// before "now" (in millis()) they started.
struct Unresolved {
  String path;
  uint32_t startMillis;
};
Unresolved unresolved[4];
int unresolvedCount = 0;

struct Guard {
  Guard() { xSemaphoreTake(lock, portMAX_DELAY); }
  ~Guard() { xSemaphoreGive(lock); }
};

uint32_t epochNow() {
  const time_t now = time(nullptr);
  return now > kValidEpoch ? static_cast<uint32_t>(now) : 0;
}

bool writeHeader(fs::File& f, const ridefile::Header& h) {
  f.seek(0);
  return f.write(reinterpret_cast<const uint8_t*>(&h), sizeof(h)) == sizeof(h);
}

}  // namespace

bool begin() {
  lock = xSemaphoreCreateMutex();
  if (!LittleFS.begin(true)) return false;
  if (!LittleFS.exists(kDir)) LittleFS.mkdir(kDir);
  return true;
}

void start(uint16_t ftp, uint8_t efforts) {
  Guard g;
  if (current) current.close();
  Preferences prefs;
  prefs.begin("caycle", false);
  const uint32_t seq = prefs.getUInt("rideseq", 0) + 1;
  prefs.putUInt("rideseq", seq);
  prefs.end();

  char path[32];
  snprintf(path, sizeof(path), "%s/%06lu.cyl", kDir, static_cast<unsigned long>(seq));
  currentPath = path;
  current = LittleFS.open(path, "w+");
  startMillis = millis();
  samples = 0;
  ridefile::Header h;
  ridefile::initHeader(h, epochNow(), ftp, efforts);
  if (!current || !writeHeader(current, h)) {
    Serial.println("recorder: cannot create ride file");
    current.close();
    return;
  }
  Serial.printf("recording to %s\n", path);
}

void sample(const fec::Metrics& m) {
  Guard g;
  if (!current) return;
  ridefile::Sample s;
  s.t = static_cast<uint16_t>((millis() - startMillis) / 1000);
  s.power = static_cast<uint16_t>(m.power > 0 ? m.power : 0);
  s.cadence = static_cast<uint8_t>(m.cadence > 0 ? (m.cadence > 255 ? 255 : m.cadence) : 0);
  s.heartRate = 0;
  s.speedCkmh = static_cast<uint16_t>(m.speedKmh * 100 + 0.5f);
  current.seek(sizeof(ridefile::Header) + samples * sizeof(s));
  if (current.write(reinterpret_cast<const uint8_t*>(&s), sizeof(s)) == sizeof(s)) {
    samples++;
    if (samples % kFlushEvery == 0) current.flush();
  }
}

void finish() {
  Guard g;
  if (!current) return;
  if (samples < kMinSamples) {
    current.close();
    LittleFS.remove(currentPath);
    Serial.printf("ride too short (%lu s), discarded\n", static_cast<unsigned long>(samples));
    currentPath = "";
    return;
  }
  ridefile::Header h;
  current.seek(0);
  current.read(reinterpret_cast<uint8_t*>(&h), sizeof(h));
  if (h.startEpoch == 0) {
    const uint32_t now = epochNow();
    if (now) {
      h.startEpoch = now - (millis() - startMillis) / 1000;
      writeHeader(current, h);
    } else if (unresolvedCount < 4) {
      unresolved[unresolvedCount++] = {currentPath, startMillis};
    }
  }
  current.close();
  Serial.printf("ride saved: %s, %lu s\n", currentPath.c_str(), static_cast<unsigned long>(samples));
  currentPath = "";
}

bool recording() {
  Guard g;
  return static_cast<bool>(current);
}

void resolveStartTimes() {
  Guard g;
  const uint32_t now = epochNow();
  if (!now) return;
  for (int i = 0; i < unresolvedCount; i++) {
    fs::File f = LittleFS.open(unresolved[i].path, "r+");
    ridefile::Header h;
    if (f && f.read(reinterpret_cast<uint8_t*>(&h), sizeof(h)) == sizeof(h) && h.startEpoch == 0) {
      h.startEpoch = now - (millis() - unresolved[i].startMillis) / 1000;
      writeHeader(f, h);
    }
    f.close();
  }
  unresolvedCount = 0;
}

int pending(String* paths, int max) {
  Guard g;
  fs::File dir = LittleFS.open(kDir);
  int n = 0;
  for (fs::File f = dir.openNextFile(); f && n < max; f = dir.openNextFile()) {
    String path = String(kDir) + "/" + f.name();
    f.close();
    if (path == currentPath || !path.endsWith(".cyl")) continue;
    // Keep the list sorted: names are zero-padded sequence numbers.
    int i = n++;
    while (i > 0 && paths[i - 1] > path) {
      paths[i] = paths[i - 1];
      i--;
    }
    paths[i] = path;
  }
  return n;
}

void remove(const String& path) {
  Guard g;
  LittleFS.remove(path);
}

size_t freeBytes() { return LittleFS.totalBytes() - LittleFS.usedBytes(); }

bool FileSource::open(const String& path) {
  file_ = LittleFS.open(path, "r");
  if (!file_ || file_.read(reinterpret_cast<uint8_t*>(&header_), sizeof(header_)) != sizeof(header_) ||
      !ridefile::validHeader(header_)) {
    file_.close();
    return false;
  }
  count_ = (file_.size() - sizeof(header_)) / sizeof(ridefile::Sample);
  next_ = 0;
  return true;
}

void FileSource::close() { file_.close(); }

bool FileSource::get(uint32_t i, ridefile::Sample& out) {
  if (i >= count_) return false;
  if (i != next_) file_.seek(sizeof(header_) + i * sizeof(ridefile::Sample));
  if (file_.read(reinterpret_cast<uint8_t*>(&out), sizeof(out)) != sizeof(out)) return false;
  next_ = i + 1;
  return true;
}

}  // namespace recorder
