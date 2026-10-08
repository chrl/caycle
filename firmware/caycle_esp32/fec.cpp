#include "fec.h"

#include <string.h>

namespace fec {
namespace {

constexpr uint8_t kSync = 0xA4;
constexpr uint8_t kPayloadLen = 0x09;
constexpr uint8_t kMsgAcknowledged = 0x4F;
constexpr uint8_t kChannel = 0x05;

uint8_t checksum(const uint8_t* b, size_t n) {
  uint8_t c = 0;
  for (size_t i = 0; i < n; i++) c ^= b[i];
  return c;
}

float clampf(float v, float lo, float hi) { return v < lo ? lo : (v > hi ? hi : v); }

}  // namespace

void encode(const Page page, uint8_t out[kFrameLen]) {
  out[0] = kSync;
  out[1] = kPayloadLen;
  out[2] = kMsgAcknowledged;
  out[3] = kChannel;
  memcpy(out + 4, page, 8);
  out[12] = checksum(out, 12);
}

bool decode(const uint8_t* frame, size_t len, Page out) {
  if (len < kFrameLen || frame[0] != kSync || frame[1] != kPayloadLen) return false;
  if (checksum(frame, 12) != frame[12]) return false;
  memcpy(out, frame + 4, 8);
  return true;
}

void apply(const Page p, Metrics& m) {
  switch (p[0]) {
    case kPageGeneral: {
      uint16_t mmps = p[4] | (p[5] << 8);  // 0.001 m/s
      m.speedKmh = mmps * 0.0036f;
      break;
    }
    case kPageTrainer: {
      m.cadence = p[2] == 0xFF ? -1 : p[2];
      int power = p[5] | ((p[6] & 0x0F) << 8);
      m.power = power == 0xFFF ? -1 : power;
      m.target = static_cast<TargetStatus>(p[7] & 0x03);
      break;
    }
  }
}

void basicResistance(float percent, Page out) {
  const uint8_t v = static_cast<uint8_t>(clampf(percent, 0, 100) * 2 + 0.5f);
  const uint8_t page[8] = {kPageBasicResistance, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, v};
  memcpy(out, page, 8);
}

void targetPower(float watts, Page out) {
  const uint16_t v = static_cast<uint16_t>(clampf(watts, 0, 4000) * 4 + 0.5f);
  const uint8_t page[8] = {kPageTargetPower, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
                           static_cast<uint8_t>(v), static_cast<uint8_t>(v >> 8)};
  memcpy(out, page, 8);
}

void userConfig(float userKg, float bikeKg, float wheelM, Page out) {
  const uint16_t user = static_cast<uint16_t>(clampf(userKg, 0, 655.34f) * 100 + 0.5f);
  const uint16_t bike = static_cast<uint16_t>(clampf(bikeKg, 0, 50) * 20 + 0.5f) & 0x0FFF;
  const uint8_t wheel = static_cast<uint8_t>(clampf(wheelM, 0, 2.54f) * 100 + 0.5f);
  const uint8_t page[8] = {kPageUserConfig,
                           static_cast<uint8_t>(user),
                           static_cast<uint8_t>(user >> 8),
                           0xFF,
                           static_cast<uint8_t>(0x0F | ((bike & 0x0F) << 4)),  // wheel offset unset
                           static_cast<uint8_t>(bike >> 4),
                           wheel,
                           0x00};  // gear ratio unset
  memcpy(out, page, 8);
}

}  // namespace fec
