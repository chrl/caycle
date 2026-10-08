#include "battery.h"

#include <Arduino.h>

namespace battery {
namespace {

constexpr int kReadPin = 1;
constexpr int kCtrlPin = 37;
constexpr float kDivider = (390.0f + 100.0f) / 100.0f;
constexpr uint32_t kEveryMs = 5000;
constexpr float kMinPlausible = 2.8f, kMaxPlausible = 4.6f;

// Board revisions differ in whether GPIO37 enables the divider when LOW or
// HIGH, so begin() finds out which level gives a battery-like reading.
int enableLevel = LOW;
float smoothed = 0;
uint32_t lastRead = 0;

float measure(int level) {
  digitalWrite(kCtrlPin, level);
  delay(5);  // let the divider settle
  uint32_t mv = 0;
  for (int i = 0; i < 16; i++) mv += analogReadMilliVolts(kReadPin);
  digitalWrite(kCtrlPin, level == LOW ? HIGH : LOW);  // divider off between reads
  return mv / 16.0f / 1000.0f * kDivider;
}

bool plausible(float v) { return v >= kMinPlausible && v <= kMaxPlausible; }

}  // namespace

void begin() {
  pinMode(kCtrlPin, OUTPUT);
  analogReadResolution(12);
  const float low = measure(LOW);
  const float high = measure(HIGH);
  Serial.printf("battery: %.2f V with ADC_Ctrl LOW, %.2f V with HIGH\n", low, high);
  enableLevel = plausible(high) && !plausible(low) ? HIGH : LOW;
  const float v = enableLevel == HIGH ? high : low;
  smoothed = plausible(v) ? v : 0;
  lastRead = millis();
}

void loop() {
  if (millis() - lastRead < kEveryMs) return;
  lastRead = millis();
  const float v = measure(enableLevel);
  if (!plausible(v)) {
    smoothed = 0;
  } else {
    smoothed = smoothed == 0 ? v : smoothed * 0.8f + v * 0.2f;
  }
}

float volts() { return smoothed; }

int percent() {
  if (smoothed == 0) return -1;
  // Piecewise-linear LiPo discharge curve (resting voltage).
  static const float kV[] = {3.30f, 3.60f, 3.70f, 3.80f, 3.90f, 4.00f, 4.10f, 4.20f};
  static const int kP[] = {0, 10, 25, 45, 60, 75, 90, 100};
  if (smoothed <= kV[0]) return 0;
  for (int i = 1; i < 8; i++) {
    if (smoothed <= kV[i]) {
      return kP[i - 1] + static_cast<int>((smoothed - kV[i - 1]) / (kV[i] - kV[i - 1]) * (kP[i] - kP[i - 1]));
    }
  }
  return 100;
}

}  // namespace battery
