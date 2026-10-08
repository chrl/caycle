// Records workouts to flash (LittleFS) as ride files, one sample per second
// of pedaling, for upload to Strava later.
#pragma once

#include <Arduino.h>
#include <FS.h>

#include "fec.h"
#include "ridefile.h"

namespace recorder {

bool begin();  // mounts the filesystem (formats it on first use)

void start(uint16_t ftp, uint8_t efforts);
void sample(const fec::Metrics& m);  // call once per second while pedaling
// Closes the ride. Rides shorter than a minute are discarded.
void finish();
bool recording();

// Sets the start time of rides finished in this boot that were recorded
// before the clock was synced. Call once the time is valid.
void resolveStartTimes();

// Finished rides waiting for upload (oldest first), excluding the one being recorded.
int pending(String* paths, int max);
void remove(const String& path);
size_t freeBytes();

// Read access to a ride file for uploading.
class FileSource : public ridefile::Source {
 public:
  bool open(const String& path);
  void close();
  const ridefile::Header& header() const { return header_; }
  uint32_t count() override { return count_; }
  bool get(uint32_t i, ridefile::Sample& out) override;

 private:
  fs::File file_;
  ridefile::Header header_{};
  uint32_t count_ = 0;
  uint32_t next_ = 0;  // index of the sample at the file position
};

}  // namespace recorder
