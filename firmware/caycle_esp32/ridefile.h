// Ride log format and the streaming Strava upload body. Pure logic (no
// Arduino), unit-tested on the host.
//
// A ride file is a 12-byte header followed by one 8-byte sample per second
// of riding. Uploads are generated on the fly as TCX inside a
// multipart/form-data body, so an hour (~1 MB of TCX) never sits in RAM.
#pragma once

#include <stddef.h>
#include <stdint.h>

namespace ridefile {

#pragma pack(push, 1)
struct Header {
  char magic[4];        // "CYL1"
  uint32_t startEpoch;  // unix seconds of the first sample, 0 if unknown
  uint16_t ftp;
  uint8_t efforts;      // work intervals in the workout
  uint8_t reserved;
};

struct Sample {
  uint16_t t;           // seconds since start (wall clock, so pauses show as gaps)
  uint16_t power;       // W
  uint8_t cadence;      // rpm
  uint8_t heartRate;    // bpm, 0 if none
  uint16_t speedCkmh;   // 0.01 km/h
};
#pragma pack(pop)

static_assert(sizeof(Header) == 12, "header layout");
static_assert(sizeof(Sample) == 8, "sample layout");

void initHeader(Header& h, uint32_t startEpoch, uint16_t ftp, uint8_t efforts);
bool validHeader(const Header& h);

// Random access to a ride's samples (a file on the device, a vector in tests).
class Source {
 public:
  virtual ~Source() = default;
  virtual uint32_t count() = 0;
  virtual bool get(uint32_t i, Sample& out) = 0;
};

struct Totals {
  uint32_t seconds = 0;  // moving time: one sample per second
  float distanceM = 0;
  uint32_t kcal = 0;     // ~1 kcal burned per kJ of work on a bike
  uint32_t avgPower = 0;
};

Totals totals(Source& src);

// "2026-10-09T18:30:05Z"
void formatTime(uint32_t epoch, char out[21]);

// Pull-based generator of the TCX document.
class Tcx {
 public:
  Tcx(Source& src, const Header& h);
  size_t read(uint8_t* buf, size_t n);  // 0 at the end
  void rewind();

 private:
  bool nextPiece();

  Source& src_;
  Header h_;
  Totals totals_;
  int stage_ = 0;
  uint32_t index_ = 0;
  float distance_ = 0;
  char piece_[512];
  size_t len_ = 0, off_ = 0;
};

// multipart/form-data body with the upload fields and the TCX as "file".
class UploadBody {
 public:
  UploadBody(Source& src, const Header& h, const char* name, const char* description);
  size_t read(uint8_t* buf, size_t n);
  void rewind();
  size_t size();  // total bytes (generates the body once)
  const char* contentType() const { return contentType_; }

 private:
  Tcx tcx_;
  char head_[1024];
  char tail_[64];
  char contentType_[80];
  size_t headLen_, tailLen_;
  int stage_ = 0;
  size_t off_ = 0;
  size_t size_ = 0;
};

}  // namespace ridefile
