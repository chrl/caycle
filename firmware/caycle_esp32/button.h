// Turns raw button samples into short / double / long presses. Pure logic:
// feed it the pin level and the time, so it can be tested on the host.
#pragma once

#include <stdint.h>

enum class Press : uint8_t { None, Short, Double, Long };

class Button {
 public:
  static constexpr uint32_t kDebounceMs = 30;
  static constexpr uint32_t kLongMs = 1000;
  static constexpr uint32_t kDoubleGapMs = 350;

  // pressed: current (debounced or raw) level; now: milliseconds.
  Press update(bool pressed, uint32_t now);

 private:
  bool stable_ = false;      // debounced level
  bool last_ = false;        // last raw level
  uint32_t changedAt_ = 0;   // when the raw level last changed
  uint32_t downAt_ = 0;      // when the current press started
  uint32_t upAt_ = 0;        // when the last short press was released
  bool longFired_ = false;
  bool pendingShort_ = false;
};
