#include "net.h"

#include <HTTPClient.h>
#include <Preferences.h>
#include <WiFi.h>
#include <WiFiClientSecure.h>
#include <time.h>

#include "certs.h"
#include "json.h"
#include "recorder.h"
#include "ridefile.h"

namespace net {
namespace {

constexpr const char* kTokenUrl = "https://www.strava.com/oauth/token";
constexpr const char* kUploadUrl = "https://www.strava.com/api/v3/uploads";
constexpr uint32_t kWifiTimeoutMs = 20000;
constexpr uint32_t kClockTimeoutMs = 10000;
constexpr int kMaxPending = 16;

constexpr uint32_t kSync = 1, kCheck = 2;

SemaphoreHandle_t lock = nullptr;
TaskHandle_t task = nullptr;
Preferences prefs;  // namespace "caycle-net", guarded by lock
String statusText;
volatile bool working = false;

struct Guard {
  Guard() { xSemaphoreTake(lock, portMAX_DELAY); }
  ~Guard() { xSemaphoreGive(lock); }
};

void setStatus(const String& s) {
  {
    Guard g;
    statusText = s;
  }
  if (s.length()) Serial.printf("net: %s\n", s.c_str());
}

String pref(const char* key) {
  Guard g;
  return prefs.getString(key, "");
}

bool connectWifi() {
  const String ssid = pref("ssid");
  if (ssid.isEmpty()) {
    setStatus("NO WIFI SET");
    return false;
  }
  if (WiFi.status() == WL_CONNECTED) return true;
  setStatus("WIFI...");
  WiFi.mode(WIFI_STA);
  WiFi.begin(ssid.c_str(), pref("pass").c_str());
  const uint32_t start = millis();
  while (WiFi.status() != WL_CONNECTED) {
    if (millis() - start > kWifiTimeoutMs) {
      setStatus("NO WIFI");
      return false;
    }
    delay(200);
  }
  return true;
}

void wifiOff() {
  WiFi.disconnect(true);
  WiFi.mode(WIFI_OFF);
}

bool syncClock() {
  if (time(nullptr) > 1700000000) return true;
  configTime(0, 0, "pool.ntp.org", "time.google.com");
  const uint32_t start = millis();
  while (time(nullptr) < 1700000000) {
    if (millis() - start > kClockTimeoutMs) return false;
    delay(200);
  }
  Serial.println("net: clock synced");
  return true;
}

// All requests share one TLS client and close their connection when done:
// two TLS sessions at once don't fit next to Bluetooth and Wi-Fi in RAM.
void beginRequest(HTTPClient& http, WiFiClientSecure& client, const String& url) {
  http.setReuse(false);
  http.setTimeout(30000);
  http.begin(client, url);
}

// Returns a valid access token, refreshing (and saving the rotated refresh
// token) when needed. force refreshes even if the current one is valid.
bool accessToken(WiFiClientSecure& client, String* token, bool force) {
  String id, secret, refresh, access;
  int64_t expires;
  {
    Guard g;
    id = prefs.getString("cid", "");
    secret = prefs.getString("csecret", "");
    refresh = prefs.getString("refresh", "");
    access = prefs.getString("access", "");
    expires = prefs.getLong64("expires", 0);
  }
  if (id.isEmpty() || refresh.isEmpty()) {
    setStatus("STRAVA NOT SET");
    return false;
  }
  if (!force && access.length() && expires > time(nullptr) + 300) {
    *token = access;
    return true;
  }

  HTTPClient http;
  beginRequest(http, client, kTokenUrl);
  http.addHeader("Content-Type", "application/x-www-form-urlencoded");
  const int code = http.POST("client_id=" + id + "&client_secret=" + secret + "&grant_type=refresh_token&refresh_token=" + refresh);
  const String body = http.getString();
  http.end();
  char newAccess[96], newRefresh[96];
  int64_t newExpires;
  if (code != 200 || !json::get(body.c_str(), "access_token", newAccess, sizeof(newAccess)) ||
      !json::get(body.c_str(), "refresh_token", newRefresh, sizeof(newRefresh)) ||
      !json::getInt(body.c_str(), "expires_at", &newExpires)) {
    Serial.printf("net: token refresh failed (%d): %s\n", code, body.c_str());
    setStatus(code == 400 || code == 401 ? "STRAVA LOGIN?" : "STRAVA ERROR");
    return false;
  }
  {
    Guard g;
    prefs.putString("access", newAccess);
    prefs.putString("refresh", newRefresh);
    prefs.putLong64("expires", newExpires);
  }
  *token = newAccess;
  return true;
}

// Streams an UploadBody into HTTPClient.
class BodyStream : public Stream {
 public:
  explicit BodyStream(ridefile::UploadBody& body) : body_(body), left_(body.size()) {}
  int available() override { return left_ > 0x7FFFFFFF ? 0x7FFFFFFF : static_cast<int>(left_); }
  size_t readBytes(char* buf, size_t n) override {
    const size_t k = body_.read(reinterpret_cast<uint8_t*>(buf), n);
    left_ -= k;
    return k;
  }
  int read() override {
    char c;
    return readBytes(&c, 1) == 1 ? static_cast<uint8_t>(c) : -1;
  }
  int peek() override { return -1; }
  size_t write(uint8_t) override { return 0; }
  void flush() override {}

 private:
  ridefile::UploadBody& body_;
  size_t left_;
};

enum class Result { Uploaded, Duplicate, Failed };

Result uploadOne(WiFiClientSecure& client, const String& path, const String& token, String* activityUrl) {
  recorder::FileSource src;
  if (!src.open(path)) {
    Serial.printf("net: %s is not a ride file, removing\n", path.c_str());
    return Result::Duplicate;  // nothing worth keeping
  }
  const ridefile::Header& h = src.header();
  if (h.startEpoch == 0) {
    Serial.printf("net: %s has no start time, skipping\n", path.c_str());
    src.close();
    return Result::Failed;
  }
  char description[64];
  snprintf(description, sizeof(description), "%u efforts at FTP %u W", h.efforts, h.ftp);
  ridefile::UploadBody body(src, h, "Intervals", description);
  BodyStream stream(body);

  HTTPClient http;
  beginRequest(http, client, kUploadUrl);
  http.addHeader("Authorization", "Bearer " + token);
  http.addHeader("Content-Type", body.contentType());
  const size_t size = body.size();
  Serial.printf("net: uploading %s (%u bytes, %u bytes free RAM)\n", path.c_str(), static_cast<unsigned>(size),
                static_cast<unsigned>(ESP.getFreeHeap()));
  const int code = http.sendRequest("POST", &stream, size);
  const String resp = http.getString();
  http.end();
  src.close();

  char err[160] = "", idStr[24] = "";
  json::get(resp.c_str(), "error", err, sizeof(err));
  if (strstr(err, "duplicate")) {
    Serial.printf("net: already on Strava: %s\n", err);
    return Result::Duplicate;
  }
  if (code != 201 || !json::get(resp.c_str(), "id_str", idStr, sizeof(idStr))) {
    Serial.printf("net: upload failed (%d): %s\n", code, resp.c_str());
    return Result::Failed;
  }

  // Strava processes uploads asynchronously; poll until the activity exists.
  setStatus("PROCESSING...");
  for (int i = 0; i < 20; i++) {
    delay(1500);
    HTTPClient poll;
    beginRequest(poll, client, String(kUploadUrl) + "/" + idStr);
    poll.addHeader("Authorization", "Bearer " + token);
    const int pc = poll.GET();
    const String pr = poll.getString();
    poll.end();
    Serial.printf("net: poll %d: %d %s\n", i + 1, pc, pr.c_str());
    if (pc != 200) continue;
    if (json::get(pr.c_str(), "error", err, sizeof(err))) {
      Serial.printf("net: Strava rejected the ride: %s\n", err);
      return strstr(err, "duplicate") ? Result::Duplicate : Result::Failed;
    }
    int64_t activity;
    if (json::getInt(pr.c_str(), "activity_id", &activity)) {
      *activityUrl = "https://www.strava.com/activities/" + String(static_cast<long long>(activity));
      return Result::Uploaded;
    }
  }
  Serial.println("net: Strava is still processing; it will show up shortly");
  return Result::Uploaded;
}

void sync() {
  String paths[kMaxPending];
  const int n = recorder::pending(paths, kMaxPending);
  if (!connectWifi()) return;
  if (!syncClock()) {
    setStatus("NO CLOCK");
    return;
  }
  recorder::resolveStartTimes();
  if (n == 0) {
    setStatus("");
    return;
  }
  WiFiClientSecure client;
  client.setCACert(kRootCAs);
  String token;
  if (!accessToken(client, &token, false)) return;
  int done = 0, failed = 0;
  for (int i = 0; i < n; i++) {
    setStatus(n > 1 ? "UPLOAD " + String(i + 1) + "/" + String(n) : String("UPLOADING..."));
    String url;
    switch (uploadOne(client, paths[i], token, &url)) {
      case Result::Uploaded:
        Serial.printf("net: uploaded %s %s\n", paths[i].c_str(), url.c_str());
        recorder::remove(paths[i]);
        done++;
        break;
      case Result::Duplicate:
        recorder::remove(paths[i]);
        break;
      case Result::Failed:
        failed++;
        break;
    }
  }
  setStatus(failed ? "UPLOAD FAILED" : (done ? "ON STRAVA" : ""));
}

void run(void*) {
  for (;;) {
    uint32_t what = 0;
    xTaskNotifyWait(0, UINT32_MAX, &what, portMAX_DELAY);
    working = true;
    if (what & kSync) sync();
    if (what & kCheck) {
      WiFiClientSecure client;
      client.setCACert(kRootCAs);
      String token;
      if (connectWifi() && syncClock() && accessToken(client, &token, true)) setStatus("STRAVA OK");
    }
    wifiOff();
    working = false;
  }
}

}  // namespace

void begin() {
  lock = xSemaphoreCreateMutex();
  {
    Guard g;
    prefs.begin("caycle-net", false);
  }
  WiFi.mode(WIFI_OFF);
  // TLS needs a generous stack.
  xTaskCreatePinnedToCore(run, "net", 16384, nullptr, 1, &task, 0);
  requestSync();  // set the clock and upload anything left from earlier rides
}

void requestSync() {
  if (task) xTaskNotify(task, kSync, eSetBits);
}

void requestCheck() {
  if (task) xTaskNotify(task, kCheck, eSetBits);
}

String status() {
  Guard g;
  return statusText;
}

bool busy() { return working; }

void setWifiSsid(const String& ssid) {
  Guard g;
  prefs.putString("ssid", ssid);
}

void setWifiPassword(const String& password) {
  Guard g;
  prefs.putString("pass", password);
}

void setStrava(const String& clientId, const String& clientSecret, const String& refreshToken) {
  Guard g;
  prefs.putString("cid", clientId);
  prefs.putString("csecret", clientSecret);
  prefs.putString("refresh", refreshToken);
  prefs.remove("access");
  prefs.remove("expires");
}

String describe() {
  Guard g;
  const String ssid = prefs.getString("ssid", "");
  return "wifi " + (ssid.length() ? "\"" + ssid + "\"" : String("not set")) + ", strava " +
         (prefs.getString("refresh", "").length() ? "set" : "not set") + ", " +
         String(static_cast<unsigned>(recorder::freeBytes() / 1024)) + " KB free";
}

}  // namespace net
