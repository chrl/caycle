// ANT+ FE-C data pages as tunnelled over BLE by Tacx trainers ("FE-C over BLE").
// Port of internal/fec from the Go app; no Arduino dependencies so it can be
// unit-tested on the host.
//
// Every BLE packet carries one ANT message:
//   A4 09 <msgID> 05 <8-byte data page> <checksum = XOR of preceding bytes>
// The trainer sends broadcast messages (0x4E); we send acknowledged ones (0x4F).
#pragma once

#include <stddef.h>
#include <stdint.h>

namespace fec {

constexpr size_t kFrameLen = 13;

constexpr uint8_t kPageGeneral = 0x10;
constexpr uint8_t kPageTrainer = 0x19;
constexpr uint8_t kPageBasicResistance = 0x30;
constexpr uint8_t kPageTargetPower = 0x31;
constexpr uint8_t kPageUserConfig = 0x37;

using Page = uint8_t[8];

// Whether the trainer can reach the ERG target (page 0x19).
enum class TargetStatus : uint8_t { Ok = 0, SpeedLow = 1, SpeedHigh = 2, Unknown = 3 };

// Latest values; -1 means "not reported".
struct Metrics {
  int power = -1;     // W
  int cadence = -1;   // rpm
  float speedKmh = 0;
  TargetStatus target = TargetStatus::Ok;
};

// Wraps a data page into an acknowledged ANT message (13 bytes).
void encode(const Page page, uint8_t out[kFrameLen]);

// Extracts the data page from one ANT message; false if malformed.
bool decode(const uint8_t* frame, size_t len, Page out);

// Applies data pages 0x10 / 0x19 to m; other pages are ignored.
void apply(const Page page, Metrics& m);

// Control pages.
void basicResistance(float percent, Page out);  // 0-100 %, 0.5 % steps
void targetPower(float watts, Page out);         // ERG, 0.25 W steps
void userConfig(float userKg, float bikeKg, float wheelM, Page out);

}  // namespace fec
