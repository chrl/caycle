// 128x64 OLED dashboard for the Heltec WiFi LoRa 32 (V3).
#pragma once

#include <Arduino.h>

struct Screen {
  bool connected = false;
  const char* status = "";    // trainer status when not connected
  const char* header = "";    // left of the top line, e.g. "WORK 3/9"
  char right[12] = "";        // right of the top line, e.g. countdown
  float progress = -1;        // 0..1 bar under the top line; <0 hides it
  int power = -1;             // W, big number
  const char* sideLabel = ""; // small label in the right column
  char side[8] = "";          // value in the right column
  int cadence = -1;           // rpm
  float speedKmh = 0;
  float distanceKm = -1;          // <0 hides it
  const char* warning = nullptr;  // blinks on the bottom line
  String note;                    // replaces the bottom line, e.g. upload status
  char battery[12] = "";          // e.g. "3.92V", shown on the searching screen
};

namespace display {

bool begin();
void splash(const char* line, const char* corner = "");
void draw(const Screen& s);

}  // namespace display
