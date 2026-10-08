// Host-side tests for the firmware's pure logic: `make -C firmware test`.
#include <math.h>
#include <stdio.h>
#include <string.h>

#include "../caycle_esp32/button.h"
#include "../caycle_esp32/fec.h"
#include "../caycle_esp32/json.h"
#include "../caycle_esp32/ridefile.h"
#include "../caycle_esp32/workout.h"

static int failures = 0;

#define CHECK(cond)                                                    \
  do {                                                                 \
    if (!(cond)) {                                                     \
      fprintf(stderr, "%s:%d: CHECK failed: %s\n", __FILE__, __LINE__, #cond); \
      failures++;                                                      \
    }                                                                  \
  } while (0)

static void testFecRoundTrip() {
  fec::Page page;
  fec::basicResistance(37.5f, page);
  uint8_t frame[fec::kFrameLen];
  fec::encode(page, frame);
  const uint8_t want[] = {0xA4, 0x09, 0x4F, 0x05, 0x30, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 75};
  CHECK(memcmp(frame, want, sizeof(want)) == 0);

  fec::Page back;
  CHECK(fec::decode(frame, sizeof(frame), back));
  CHECK(memcmp(back, page, 8) == 0);
  frame[12] ^= 0xFF;
  CHECK(!fec::decode(frame, sizeof(frame), back));
  CHECK(!fec::decode(frame, 5, back));
}

static void testFecPages() {
  fec::Metrics m;
  // cadence 90, power 0x123 = 291 W, target status "speed too low".
  const uint8_t trainer[8] = {0x19, 7, 90, 0x34, 0x12, 0x23, 0x21, 0x31};
  fec::apply(trainer, m);
  CHECK(m.cadence == 90);
  CHECK(m.power == 291);
  CHECK(m.target == fec::TargetStatus::SpeedLow);

  const uint8_t invalid[8] = {0x19, 0, 0xFF, 0, 0, 0xFF, 0x0F, 0};
  fec::apply(invalid, m);
  CHECK(m.cadence == -1);
  CHECK(m.power == -1);

  // 10 m/s = 10000 mm/s = 36 km/h.
  const uint8_t general[8] = {0x10, 0x19, 40, 120, 0x10, 0x27, 0xFF, 0x20};
  fec::apply(general, m);
  CHECK(fabsf(m.speedKmh - 36.0f) < 0.01f);

  fec::Page p;
  fec::targetPower(250, p);  // 1000 = 0x03E8
  CHECK(p[0] == 0x31 && p[6] == 0xE8 && p[7] == 0x03);
  fec::userConfig(75, 9, 0.70f, p);
  const uint8_t want[8] = {0x37, 0x4C, 0x1D, 0xFF, 0x4F, 0x0B, 70, 0x00};
  CHECK(memcmp(p, want, 8) == 0);
}

static void testWorkoutPlan() {
  for (uint32_t seed = 1; seed < 200; seed++) {
    const workout::Plan p = workout::generate(seed, 45);
    CHECK(p.totalSeconds() == 45 * 60);
    CHECK(p.steps[0].kind == workout::Kind::Warmup);
    CHECK(p.steps[p.count - 1].kind == workout::Kind::Cooldown);
    CHECK(p.workCount >= 5);
    for (int i = 0; i < p.count; i++) {
      const workout::Step& s = p.steps[i];
      if (s.kind == workout::Kind::Work) {
        CHECK(s.pct >= 100 && s.pct <= 150);
        CHECK(s.seconds >= 30 && s.seconds <= 240);
        CHECK(p.steps[i + 1].kind == workout::Kind::Recover);  // every effort gets a rest
      }
      if (s.kind == workout::Kind::Recover) {
        CHECK(s.pct >= 50 && s.pct <= 60);
        CHECK(s.seconds >= 60 && s.seconds <= 360);
      }
    }
  }
  // Same seed, same workout; different seed, different workout.
  const workout::Plan a = workout::generate(42, 30), b = workout::generate(42, 30), c = workout::generate(43, 30);
  CHECK(a.count == b.count && memcmp(a.steps, b.steps, sizeof(workout::Step) * a.count) == 0);
  CHECK(a.count != c.count || memcmp(a.steps, c.steps, sizeof(workout::Step) * a.count) != 0);
  // Too short requests are stretched to the minimum.
  CHECK(workout::generate(1, 5).totalSeconds() == 20 * 60);
}

static void testPlayer() {
  workout::Player pl;
  pl.load(workout::generate(7, 30));
  CHECK(pl.state() == workout::State::Idle);
  pl.start();
  CHECK(pl.changed());
  CHECK(!pl.changed());
  CHECK(pl.step().kind == workout::Kind::Warmup);
  CHECK(pl.targetWatts(200, 0) == 100);  // 50 % of 200 W

  pl.tick(60000, false);  // not pedaling: time stands still
  CHECK(pl.elapsedMs() == 0);
  pl.tick(119000, true);
  CHECK(pl.index() == 0 && pl.stepRemainingMs() == 1000);
  pl.tick(1500, true);  // crosses into step 2 with 500 ms carried over
  CHECK(pl.index() == 1 && pl.stepRemainingMs() == 119500);
  CHECK(pl.changed());

  pl.togglePause();
  pl.tick(10000, true);
  CHECK(pl.state() == workout::State::Paused && pl.stepRemainingMs() == 119500);
  pl.togglePause();

  pl.skip();
  pl.skip();
  CHECK(pl.step().kind == workout::Kind::Work);
  CHECK(pl.workNumber() == 1);
  const int base = pl.targetWatts(200, 0);
  CHECK(pl.targetWatts(200, 10) == base + 20);  // +10 % bias on work blocks

  for (int i = 0; i < 100 && pl.state() != workout::State::Done; i++) pl.skip();
  CHECK(pl.state() == workout::State::Done);
  pl.stop();
  CHECK(pl.state() == workout::State::Idle);
}

static Press feed(Button& b, bool pressed, uint32_t from, uint32_t to) {
  Press got = Press::None;
  for (uint32_t t = from; t <= to; t += 5) {
    const Press p = b.update(pressed, t);
    if (p != Press::None) got = p;
  }
  return got;
}

static void testButton() {
  {
    Button b;
    CHECK(feed(b, true, 0, 150) == Press::None);
    CHECK(feed(b, false, 155, 400) == Press::None);  // still waiting for a possible double
    CHECK(feed(b, false, 405, 700) == Press::Short);
  }
  {
    Button b;
    feed(b, true, 0, 100);
    feed(b, false, 105, 250);
    feed(b, true, 255, 350);
    CHECK(feed(b, false, 355, 1500) == Press::Double);
  }
  {
    Button b;
    CHECK(feed(b, true, 0, 1200) == Press::Long);
    CHECK(feed(b, false, 1205, 2000) == Press::None);  // no extra short after a long press
  }
  {
    Button b;  // a 10 ms glitch is ignored
    feed(b, true, 0, 10);
    CHECK(feed(b, false, 15, 1000) == Press::None);
  }
}

namespace {

class VecSource : public ridefile::Source {
 public:
  ridefile::Sample samples[4000];
  uint32_t n = 0;
  uint32_t count() override { return n; }
  bool get(uint32_t i, ridefile::Sample& out) override {
    if (i >= n) return false;
    out = samples[i];
    return true;
  }
};

}  // namespace

static void testFormatTime() {
  char t[21];
  ridefile::formatTime(0, t);
  CHECK(strcmp(t, "1970-01-01T00:00:00Z") == 0);
  ridefile::formatTime(1791504000 + 3600 * 18 + 30 * 60 + 5, t);  // 2026-10-09 18:30:05 UTC
  CHECK(strcmp(t, "2026-10-09T18:30:05Z") == 0);
  ridefile::formatTime(951782400, t);  // leap day
  CHECK(strcmp(t, "2000-02-29T00:00:00Z") == 0);
}

static void testRideFile() {
  static VecSource src;
  src.n = 3600;
  for (uint32_t i = 0; i < src.n; i++) {
    // 36 km/h = 10 m/s, 200 W; a 60 s pause after 30 minutes.
    src.samples[i] = {static_cast<uint16_t>(i < 1800 ? i : i + 60), 200, 90, static_cast<uint8_t>(i % 2 ? 140 : 0), 3600};
  }
  ridefile::Header h;
  ridefile::initHeader(h, 1791504000, 150, 8);
  CHECK(ridefile::validHeader(h));

  const ridefile::Totals t = ridefile::totals(src);
  CHECK(t.seconds == 3600);
  CHECK(fabsf(t.distanceM - 36000) < 1);
  CHECK(t.kcal == 720 && t.avgPower == 200);

  ridefile::UploadBody body(src, h, "Random intervals", "8 efforts");
  const size_t size = body.size();
  static uint8_t buf[2 * 1024 * 1024];
  size_t got = 0, k;
  while ((k = body.read(buf + got, 777)) > 0) got += k;  // odd chunk size on purpose
  CHECK(got == size);
  buf[got] = 0;
  const char* text = reinterpret_cast<const char*>(buf);
  CHECK(strstr(text, "name=\"data_type\"\r\n\r\ntcx\r\n") != nullptr);
  CHECK(strstr(text, "name=\"trainer\"\r\n\r\n1\r\n") != nullptr);
  CHECK(strstr(text, "caycle-1791504000.tcx") != nullptr);
  CHECK(strstr(text, "<Id>2026-10-09T00:00:00Z</Id>") != nullptr);
  CHECK(strstr(text, "<TotalTimeSeconds>3600</TotalTimeSeconds><DistanceMeters>36000.0</DistanceMeters>") != nullptr);
  // The pause shows as a time gap: sample 1800 is at t = 1860 s = 00:31:00.
  CHECK(strstr(text, "<Time>2026-10-09T00:31:00Z</Time><DistanceMeters>18010.0</DistanceMeters>") != nullptr);
  CHECK(strstr(text, "<HeartRateBpm><Value>140</Value></HeartRateBpm>") != nullptr);
  CHECK(strstr(text, "<ns3:Speed>10.00</ns3:Speed><ns3:Watts>200</ns3:Watts>") != nullptr);
  CHECK(strcmp(text + got - 25, "\r\n--caycle-7d1e3b9a4f--\r\n") == 0);

  // Re-reading after rewind gives the same bytes.
  body.rewind();
  size_t again = 0;
  uint8_t scratch[4096];
  while ((k = body.read(scratch, sizeof(scratch))) > 0) again += k;
  CHECK(again == size);

  // Hand the TCX part to the Makefile's XML check.
  const char* start = strstr(text, "<?xml");
  const char* end = strstr(text, "</TrainingCenterDatabase>");
  if (start && end) {
    FILE* f = fopen("test/ride.tcx", "wb");
    fwrite(start, 1, static_cast<size_t>(end - start) + strlen("</TrainingCenterDatabase>"), f);
    fclose(f);
  }
}

static void testJson() {
  const char* doc =
      "{\"token_type\":\"Bearer\",\"expires_at\":1791520000,\"refresh_token\":\"abc\\\"def\","
      " \"athlete\": {\"id\": 42, \"firstname\": \"Ada\"}, \"activity_id\": null,"
      " \"note\": \"has \\\"error\\\": inside\", \"error\" : \"duplicate of activity 1\", \"id_str\":\"123\"}";
  char buf[64];
  CHECK(json::get(doc, "token_type", buf, sizeof(buf)) && strcmp(buf, "Bearer") == 0);
  CHECK(json::get(doc, "refresh_token", buf, sizeof(buf)) && strcmp(buf, "abc\"def") == 0);
  CHECK(json::get(doc, "firstname", buf, sizeof(buf)) && strcmp(buf, "Ada") == 0);
  CHECK(json::get(doc, "error", buf, sizeof(buf)) && strcmp(buf, "duplicate of activity 1") == 0);
  CHECK(!json::get(doc, "activity_id", buf, sizeof(buf)));
  CHECK(!json::get(doc, "missing", buf, sizeof(buf)));
  int64_t v;
  CHECK(json::getInt(doc, "expires_at", &v) && v == 1791520000);
  CHECK(json::getInt(doc, "id_str", &v) && v == 123);
  CHECK(!json::getInt(doc, "activity_id", &v));
  CHECK(json::get(doc, "token_type", buf, 4) && strcmp(buf, "Bea") == 0);  // truncated safely
}

int main() {
  testFormatTime();
  testRideFile();
  testJson();
  testFecRoundTrip();
  testFecPages();
  testWorkoutPlan();
  testPlayer();
  testButton();
  if (failures) {
    fprintf(stderr, "%d check(s) failed\n", failures);
    return 1;
  }
  printf("all firmware tests passed\n");
  return 0;
}
