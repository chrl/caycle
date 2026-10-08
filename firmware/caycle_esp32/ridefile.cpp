#include "ridefile.h"

#include <stdio.h>
#include <string.h>

namespace ridefile {

void initHeader(Header& h, uint32_t startEpoch, uint16_t ftp, uint8_t efforts) {
  memcpy(h.magic, "CYL1", 4);
  h.startEpoch = startEpoch;
  h.ftp = ftp;
  h.efforts = efforts;
  h.reserved = 0;
}

bool validHeader(const Header& h) { return memcmp(h.magic, "CYL1", 4) == 0; }

Totals totals(Source& src) {
  Totals t;
  Sample s;
  uint64_t joules = 0;
  const uint32_t n = src.count();
  for (uint32_t i = 0; i < n; i++) {
    if (!src.get(i, s)) break;
    t.seconds++;
    t.distanceM += s.speedCkmh / 360.0f;  // (km/h / 100) / 3.6 m/s for one second
    joules += s.power;
  }
  t.kcal = static_cast<uint32_t>(joules / 1000);
  t.avgPower = t.seconds ? static_cast<uint32_t>(joules / t.seconds) : 0;
  return t;
}

void formatTime(uint32_t epoch, char out[21]) {
  // Days since 1970-01-01 to civil date (Howard Hinnant's algorithm).
  int64_t z = epoch / 86400 + 719468;
  const uint32_t secs = epoch % 86400;
  const int64_t era = z / 146097;
  const uint32_t doe = static_cast<uint32_t>(z - era * 146097);
  const uint32_t yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365;
  const uint32_t doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
  const uint32_t mp = (5 * doy + 2) / 153;
  const uint32_t d = doy - (153 * mp + 2) / 5 + 1;
  const uint32_t m = mp < 10 ? mp + 3 : mp - 9;
  const int64_t y = static_cast<int64_t>(yoe) + era * 400 + (m <= 2);
  snprintf(out, 21, "%04d-%02u-%02uT%02u:%02u:%02uZ", static_cast<int>(y), static_cast<unsigned>(m),
           static_cast<unsigned>(d), static_cast<unsigned>(secs / 3600), static_cast<unsigned>(secs / 60 % 60),
           static_cast<unsigned>(secs % 60));
}

Tcx::Tcx(Source& src, const Header& h) : src_(src), h_(h) { rewind(); }

void Tcx::rewind() {
  totals_ = totals(src_);
  stage_ = 0;
  index_ = 0;
  distance_ = 0;
  len_ = off_ = 0;
}

bool Tcx::nextPiece() {
  char t[21];
  int n = 0;
  switch (stage_) {
    case 0:
      formatTime(h_.startEpoch, t);
      n = snprintf(piece_, sizeof(piece_),
                   "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n"
                   "<TrainingCenterDatabase xmlns=\"http://www.garmin.com/xmlschemas/TrainingCenterDatabase/v2\" "
                   "xmlns:ns3=\"http://www.garmin.com/xmlschemas/ActivityExtension/v2\">"
                   "<Activities><Activity Sport=\"Biking\"><Id>%s</Id><Lap StartTime=\"%s\">"
                   "<TotalTimeSeconds>%lu</TotalTimeSeconds><DistanceMeters>%.1f</DistanceMeters>"
                   "<Calories>%lu</Calories><Intensity>Active</Intensity><TriggerMethod>Manual</TriggerMethod>"
                   "<Track>\n",
                   t, t, static_cast<unsigned long>(totals_.seconds), totals_.distanceM,
                   static_cast<unsigned long>(totals_.kcal));
      stage_ = 1;
      break;
    case 1: {
      Sample s;
      if (index_ >= src_.count() || !src_.get(index_, s)) {
        stage_ = 2;
        return nextPiece();
      }
      index_++;
      distance_ += s.speedCkmh / 360.0f;
      formatTime(h_.startEpoch + s.t, t);
      n = snprintf(piece_, sizeof(piece_), "<Trackpoint><Time>%s</Time><DistanceMeters>%.1f</DistanceMeters>", t,
                   distance_);
      if (s.heartRate) {
        n += snprintf(piece_ + n, sizeof(piece_) - n, "<HeartRateBpm><Value>%u</Value></HeartRateBpm>", s.heartRate);
      }
      n += snprintf(piece_ + n, sizeof(piece_) - n,
                    "<Cadence>%u</Cadence><Extensions><ns3:TPX><ns3:Speed>%.2f</ns3:Speed>"
                    "<ns3:Watts>%u</ns3:Watts></ns3:TPX></Extensions></Trackpoint>\n",
                    s.cadence > 254 ? 254 : s.cadence, s.speedCkmh / 360.0f, s.power);
      break;
    }
    case 2:
      n = snprintf(piece_, sizeof(piece_), "</Track></Lap></Activity></Activities></TrainingCenterDatabase>\n");
      stage_ = 3;
      break;
    default:
      return false;
  }
  len_ = static_cast<size_t>(n);
  off_ = 0;
  return true;
}

size_t Tcx::read(uint8_t* buf, size_t n) {
  size_t done = 0;
  while (done < n) {
    if (off_ == len_ && !nextPiece()) break;
    const size_t k = (len_ - off_ < n - done) ? len_ - off_ : n - done;
    memcpy(buf + done, piece_ + off_, k);
    off_ += k;
    done += k;
  }
  return done;
}

namespace {
constexpr const char* kBoundary = "caycle-7d1e3b9a4f";

int field(char* out, size_t n, const char* name, const char* value) {
  return snprintf(out, n, "--%s\r\nContent-Disposition: form-data; name=\"%s\"\r\n\r\n%s\r\n", kBoundary, name, value);
}
}  // namespace

UploadBody::UploadBody(Source& src, const Header& h, const char* name, const char* description) : tcx_(src, h) {
  char id[40];
  snprintf(id, sizeof(id), "caycle-%lu.tcx", static_cast<unsigned long>(h.startEpoch));
  int n = 0;
  n += field(head_ + n, sizeof(head_) - n, "data_type", "tcx");
  n += field(head_ + n, sizeof(head_) - n, "trainer", "1");
  n += field(head_ + n, sizeof(head_) - n, "external_id", id);
  if (name && *name) n += field(head_ + n, sizeof(head_) - n, "name", name);
  if (description && *description) n += field(head_ + n, sizeof(head_) - n, "description", description);
  n += snprintf(head_ + n, sizeof(head_) - n,
                "--%s\r\nContent-Disposition: form-data; name=\"file\"; filename=\"%s\"\r\n"
                "Content-Type: application/vnd.garmin.tcx+xml\r\n\r\n",
                kBoundary, id);
  headLen_ = static_cast<size_t>(n);
  tailLen_ = static_cast<size_t>(snprintf(tail_, sizeof(tail_), "\r\n--%s--\r\n", kBoundary));
  snprintf(contentType_, sizeof(contentType_), "multipart/form-data; boundary=%s", kBoundary);
}

void UploadBody::rewind() {
  tcx_.rewind();
  stage_ = 0;
  off_ = 0;
}

size_t UploadBody::read(uint8_t* buf, size_t n) {
  size_t done = 0;
  while (done < n && stage_ < 3) {
    if (stage_ == 1) {
      const size_t k = tcx_.read(buf + done, n - done);
      done += k;
      if (k == 0) {
        stage_ = 2;
        off_ = 0;
      }
      continue;
    }
    const char* src = stage_ == 0 ? head_ : tail_;
    const size_t len = stage_ == 0 ? headLen_ : tailLen_;
    const size_t k = (len - off_ < n - done) ? len - off_ : n - done;
    memcpy(buf + done, src + off_, k);
    off_ += k;
    done += k;
    if (off_ == len) {
      stage_++;
      off_ = 0;
    }
  }
  return done;
}

size_t UploadBody::size() {
  if (size_ == 0) {
    rewind();
    uint8_t buf[256];
    size_t k;
    while ((k = read(buf, sizeof(buf))) > 0) size_ += k;
    rewind();
  }
  return size_;
}

}  // namespace ridefile
