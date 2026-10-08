// BLE link to a Tacx trainer over the FE-C over BLE service, with automatic
// rescanning and reconnecting.
#pragma once

#include <Arduino.h>

#include "fec.h"

class Trainer {
 public:
  void begin(float riderKg, float bikeKg, float wheelM);
  void loop();  // call often; drives scanning and connecting

  bool connected() const { return state_ == State::Connected; }
  const String& name() const { return name_; }
  const char* status() const;

  // Latest data; ageMs is the time since the last notification.
  fec::Metrics metrics(uint32_t* ageMs) const;

  // Incremented on every (re)connection, so callers know to resend settings.
  uint32_t epoch() const { return epoch_; }

  bool send(const fec::Page page);

  // BLE callbacks (public so the C-style callback shims can reach them).
  void onFound(void* advertisedDevice);
  void onDisconnected();
  void onScanDone();
  void onData(const uint8_t* data, size_t len);

 private:
  enum class State : uint8_t { Idle, Scanning, Found, Connecting, Connected };

  void startScan();
  bool connect();

  volatile State state_ = State::Idle;
  String name_;
  uint32_t epoch_ = 0;
  uint32_t retryAt_ = 0;
  float riderKg_ = 75, bikeKg_ = 9, wheelM_ = 0.7f;

  fec::Metrics metrics_;
  uint32_t updatedAt_ = 0;
};
